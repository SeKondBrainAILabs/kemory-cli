package hook

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Event is what a hook payload says about the session being captured. The
// field names differ between agents; ParseEvent accepts each spelling.
type Event struct {
	SessionID      string
	Name           string // the hook event, e.g. Stop, SessionEnd, PreCompact
	TranscriptPath string
	Cwd            string
	Reason         string
	StopHookActive bool
}

// ParseEvent reads a hook payload. Missing fields stay empty; flags can fill
// them for agents that pass no payload.
func ParseEvent(payload []byte) Event {
	var raw map[string]any
	_ = json.Unmarshal(payload, &raw)
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := raw[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	e := Event{
		SessionID:      str("session_id", "sessionId", "conversation_id", "conversationId"),
		Name:           str("hook_event_name", "hookEventName", "event"),
		TranscriptPath: str("transcript_path", "transcriptPath"),
		Cwd:            str("cwd"),
		Reason:         str("reason"),
	}
	if t := str("trigger"); e.Reason == "" && t != "" && strings.EqualFold(e.Name, "PreCompact") {
		e.Reason = "compact-" + t
	}
	e.StopHookActive, _ = raw["stop_hook_active"].(bool)
	return e
}

// incremental reports whether the event fires after every turn, as opposed
// to a flush (pre-compaction, session end) that stores whatever is pending.
func (e Event) incremental() bool {
	switch strings.ToLower(e.Name) {
	case "stop", "afteragent", "agentstop":
		return true
	}
	return false
}

// UserTurns reads the user's own prompts from a transcript in JSON Lines. It
// accepts Claude Code's shape ({"type":"user","message":{"content":…}}) and
// the common {"role":"user","content":…} one. Harness-injected text (starting
// with "<" or "Caveat:") is skipped: it is not the user's intent.
func UserTurns(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var turns []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for scanner.Scan() {
		var line map[string]any
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		var content any
		switch {
		case line["type"] == "user":
			if msg, ok := line["message"].(map[string]any); ok {
				content = msg["content"]
			}
		case line["role"] == "user":
			content = line["content"]
		default:
			continue
		}
		text := strings.TrimSpace(flatten(content))
		if text != "" && !strings.HasPrefix(text, "<") && !strings.HasPrefix(text, "Caveat:") {
			turns = append(turns, text)
		}
	}
	return turns, scanner.Err()
}

func flatten(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, p := range c {
			if m, ok := p.(map[string]any); ok {
				t, _ := m["text"].(string)
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// captureState is the per-session high-water mark, shared with the plugin.
type captureState struct {
	Digest        string `json:"digest"`
	CapturedTurns int    `json:"captured_turns"`
}

// Capture is one capture to post, and the state to save once it is stored.
type Capture struct {
	Body      map[string]any
	statePath string
	nextState captureState
}

// stateDir holds one high-water file per session, under ~/.kemory.
func stateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kemory", ".captured")
}

func envInt(name string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil {
		return n
	}
	return def
}

// maxChars bounds the digest body, in characters.
const maxChars = 8000

// Plan decides what, if anything, to store for e, from the turns not stored
// yet. It returns nil when there is nothing worth storing.
func Plan(e Event, turns []string, source string) *Capture {
	if e.StopHookActive || len(turns) == 0 {
		return nil
	}
	dir := stateDir()
	name := e.SessionID
	if name == "" {
		name = "nosession"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	statePath := filepath.Join(dir, strings.ReplaceAll(name, "/", "_"))
	state := captureState{}
	if raw, err := os.ReadFile(statePath); err == nil {
		if json.Unmarshal(raw, &state) != nil {
			// Plugin markers before 0.2.0 were a bare digest.
			state = captureState{Digest: strings.TrimSpace(string(raw))}
		}
		state.CapturedTurns = max(0, state.CapturedTurns)
	}
	already := min(state.CapturedTurns, len(turns))
	fresh := turns[already:]
	if len(fresh) == 0 {
		return nil
	}
	incremental := e.incremental()
	if incremental && len(fresh) < envInt("KEMORY_CAPTURE_MIN_NEW_TURNS", 3) {
		return nil
	}
	posted := fresh
	if limit := envInt("KEMORY_CAPTURE_MAX_TURNS", 12); limit > 0 && len(posted) > limit {
		// A burst over the cap drops its oldest turns rather than storing the
		// same window twice.
		posted = posted[len(posted)-limit:]
	}
	lines := make([]string, len(posted))
	for i, t := range posted {
		lines[i] = "- " + t
	}
	body := Redact(strings.Join(lines, "\n"))
	if r := []rune(body); len(r) > maxChars {
		body = string(r[:maxChars])
	}
	when := "at session end"
	switch {
	case incremental:
		when = "mid-session"
	case strings.EqualFold(e.Name, "PreCompact") || strings.EqualFold(e.Name, "PreCompress"):
		when = "before compaction"
	}
	cwd := e.Cwd
	if cwd == "" {
		cwd = "unknown"
	}
	content := fmt.Sprintf("What was this coding session about? Session digest captured automatically "+
		"(%s, user turns only, secrets redacted, turns %d-%d).\n\ncwd: %s\n\n%s",
		when, already+len(fresh)-len(posted)+1, already+len(fresh), cwd, body)
	sum := sha256.Sum256([]byte(content))
	digest := hex.EncodeToString(sum[:])
	if digest == state.Digest {
		return nil
	}
	namespace := os.Getenv("KEMORY_CAPTURE_NAMESPACE")
	if namespace == "" {
		namespace = "user:sessions"
	}
	visibility := os.Getenv("KEMORY_CAPTURE_VISIBILITY")
	if visibility == "" {
		visibility = "user-private"
	}
	kind := "flush"
	if incremental {
		kind = "incremental"
	}
	reason := e.Reason
	if reason == "" {
		reason = "unknown"
	}
	return &Capture{
		Body: map[string]any{
			"namespace":     namespace,
			"namespace_tag": "session-capture",
			"visibility":    visibility,
			"content":       content,
			"content_type":  "text",
			"session_id":    e.SessionID,
			"metadata": map[string]any{
				"source":       source,
				"capture":      "auto",
				"capture_kind": kind,
				"turns":        len(posted),
				"end_reason":   reason,
			},
		},
		statePath: statePath,
		nextState: captureState{Digest: digest, CapturedTurns: len(turns)},
	}
}

// Stored records that the capture reached Kemory, so its turns are not
// posted again. Never call it for a capture that failed.
func (c *Capture) Stored() error {
	if err := os.MkdirAll(filepath.Dir(c.statePath), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(c.nextState)
	if err != nil {
		return err
	}
	return os.WriteFile(c.statePath, data, 0o600)
}
