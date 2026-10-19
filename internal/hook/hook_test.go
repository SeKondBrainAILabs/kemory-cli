package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactKeepsProseAndRemovesCredentials(t *testing.T) {
	cases := map[string]string{
		"we need a token for this":                    "we need a token for this",
		"set api_key=abcdef123456 here":               "set [REDACTED] here",
		"Bearer abcdefghijkl":                         "[REDACTED]",
		"key sk-abcdefghijklmnopqrstu":                "key [REDACTED]",
		"ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ":              "[REDACTED]",
		"AKIAABCDEFGHIJKLMNOP":                        "[REDACTED]",
		"password: hunter2hunter2":                    "[REDACTED]",
		"eyJabcdefghijk.eyJabcdefghijk.sigabcdefghij": "[REDACTED]",
	}
	for in, want := range cases {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildContext(t *testing.T) {
	t.Setenv("KEMORY_CONTEXT_MAX_CHARS", "")
	t.Setenv("KEMORY_CONTEXT_NAMESPACES", "")
	body := []byte(`{"namespaces": [
		{"namespace": "user:preferences", "summary": "Prefers Go."},
		{"namespace": "empty", "summary": "  "},
		{"namespace": "shared", "summary": "Deploys go through ArgoCD."}]}`)
	got := BuildContext(body, false)
	for _, want := range []string{Instruction, "- [user:preferences] Prefers Go.\n- [shared] Deploys go through ArgoCD.", trailer} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[empty]") || strings.Contains(got, consolidate) {
		t.Fatalf("empty summary or compaction nudge leaked:\n%s", got)
	}
	if !strings.Contains(BuildContext(body, true), consolidate) {
		t.Fatal("a compacted session must get the nudge")
	}

	t.Setenv("KEMORY_CONTEXT_NAMESPACES", "shared")
	if got := BuildContext(body, false); strings.Contains(got, "Prefers Go") {
		t.Fatalf("namespace filter ignored:\n%s", got)
	}

	t.Setenv("KEMORY_CONTEXT_NAMESPACES", "")
	// "- [a] " plus 484 characters is 490, under the 500 floor; b's 26 more is not.
	long := `{"namespaces": [{"namespace": "a", "summary": "` + strings.Repeat("x", 484) +
		`"}, {"namespace": "b", "summary": "` + strings.Repeat("y", 20) + `"}]}`
	t.Setenv("KEMORY_CONTEXT_MAX_CHARS", "100")
	if got := BuildContext([]byte(long), false); !strings.Contains(got, "(1 more namespace summaries omitted") {
		t.Fatalf("budget (floored at 500) not applied:\n%s", got)
	}

	for _, broken := range []string{`{"detail": "unauthorised"}`, `not json`, `{"namespaces": []}`} {
		if got := BuildContext([]byte(broken), false); got != Instruction {
			t.Fatalf("%s: want the instruction alone, got:\n%s", broken, got)
		}
	}
}

func TestOutputShapes(t *testing.T) {
	var claude struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	for _, agent := range []string{"claude-code", "codex", "gemini-cli"} {
		out, _ := Output(agent, "ctx")
		if json.Unmarshal(out, &claude) != nil || claude.HookSpecificOutput.AdditionalContext != "ctx" || claude.HookSpecificOutput.HookEventName != "SessionStart" {
			t.Fatalf("%s: %s", agent, out)
		}
	}
	var cursor map[string]string
	out, _ := Output("cursor", "ctx")
	if json.Unmarshal(out, &cursor) != nil || cursor["additional_context"] != "ctx" {
		t.Fatalf("cursor: %s", out)
	}
	if out, _ := Output("text", "ctx"); string(out) != "ctx" {
		t.Fatalf("text: %s", out)
	}
}

func TestParseEventAcceptsEachSpelling(t *testing.T) {
	e := ParseEvent([]byte(`{"session_id": "s", "hook_event_name": "PreCompact", "trigger": "auto", "transcript_path": "/t", "cwd": "/w", "stop_hook_active": true}`))
	if e.SessionID != "s" || e.Reason != "compact-auto" || e.TranscriptPath != "/t" || e.Cwd != "/w" || !e.StopHookActive {
		t.Fatalf("%+v", e)
	}
	e = ParseEvent([]byte(`{"conversationId": "c", "hookEventName": "stop", "transcriptPath": "/t"}`))
	if e.SessionID != "c" || e.Name != "stop" || e.TranscriptPath != "/t" || !e.incremental() {
		t.Fatalf("%+v", e)
	}
	if (Event{Name: "SessionEnd"}).incremental() || (Event{}).incremental() {
		t.Fatal("anything but a per-turn event flushes")
	}
}

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUserTurns(t *testing.T) {
	path := writeTranscript(t,
		`{"type": "user", "message": {"content": "first"}}`,
		`{"type": "assistant", "message": {"content": "reply"}}`,
		`{"type": "user", "message": {"content": [{"type": "text", "text": "second"}, {"type": "text", "text": "part"}]}}`,
		`{"type": "user", "message": {"content": "<system-reminder>noise</system-reminder>"}}`,
		`{"type": "user", "message": {"content": "Caveat: injected"}}`,
		`{"role": "user", "content": "third"}`,
		`not json`)
	turns, err := UserTurns(path)
	if err != nil || strings.Join(turns, "|") != "first|second part|third" {
		t.Fatalf("got %q %v", turns, err)
	}
}

func TestPlanStoresNewTurnsOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"KEMORY_CAPTURE_MIN_NEW_TURNS", "KEMORY_CAPTURE_MAX_TURNS", "KEMORY_CAPTURE_NAMESPACE", "KEMORY_CAPTURE_VISIBILITY"} {
		t.Setenv(k, "")
	}
	stop := Event{SessionID: "s1", Name: "Stop", Cwd: "/w"}

	if Plan(stop, []string{"a", "b"}, "codex") != nil {
		t.Fatal("Stop waits for three new turns")
	}
	c := Plan(stop, []string{"a", "b", "c"}, "codex")
	if c == nil {
		t.Fatal("three new turns must be stored")
	}
	content, _ := c.Body["content"].(string)
	if !strings.Contains(content, "(mid-session, user turns only, secrets redacted, turns 1-3).\n\ncwd: /w\n\n- a\n- b\n- c") {
		t.Fatalf("content:\n%s", content)
	}
	md, _ := c.Body["metadata"].(map[string]any)
	if c.Body["namespace"] != "user:sessions" || c.Body["visibility"] != "user-private" || md["source"] != "codex" || md["capture_kind"] != "incremental" {
		t.Fatalf("body: %v", c.Body)
	}
	if err := c.Stored(); err != nil {
		t.Fatal(err)
	}

	end := Event{SessionID: "s1", Name: "SessionEnd"}
	if Plan(end, []string{"a", "b", "c"}, "codex") != nil {
		t.Fatal("stored turns must not be stored again")
	}
	c = Plan(end, []string{"a", "b", "c", "d"}, "codex")
	if c == nil || !strings.Contains(c.Body["content"].(string), "(at session end, user turns only, secrets redacted, turns 4-4)") {
		t.Fatalf("a flush stores whatever is pending: %v", c)
	}

	t.Setenv("KEMORY_CAPTURE_MAX_TURNS", "2")
	c = Plan(Event{SessionID: "s2", Name: "SessionEnd"}, []string{"1", "2", "3", "4", "5"}, "codex")
	if c == nil || !strings.Contains(c.Body["content"].(string), "turns 4-5).") {
		t.Fatalf("a burst over the cap keeps its newest turns: %v", c)
	}

	if Plan(Event{SessionID: "s3", Name: "Stop", StopHookActive: true}, []string{"1", "2", "3"}, "codex") != nil {
		t.Fatal("a Stop hook started by a Stop hook must not capture")
	}
}

func TestPlanReadsThePluginsOldMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".kemory", ".captured")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s"), []byte("abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Plan(Event{SessionID: "s", Name: "SessionEnd"}, []string{"a"}, "codex") == nil {
		t.Fatal("a bare-digest marker counts as no turns stored")
	}
}

func TestUserTurnsReadsCodexAndDropsInjectedParts(t *testing.T) {
	path := writeTranscript(t,
		`{"type": "response_item", "payload": {"type": "message", "role": "user", "content": [`+
			`{"type": "input_text", "text": "<environment_context><cwd>/w</cwd></environment_context>"},`+
			`{"type": "input_text", "text": "# AGENTS.md instructions\n<INSTRUCTIONS>…</INSTRUCTIONS>"}]}}`,
		`{"type": "response_item", "payload": {"type": "message", "role": "user", "content": [{"type": "input_text", "text": "fix the build"}]}}`,
		`{"type": "response_item", "payload": {"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "done"}]}}`,
		`{"type": "response_item", "payload": {"type": "message", "role": "user", "content": [`+
			`{"type": "input_text", "text": "see this"}, {"type": "input_image", "image_url": "x"}, {"type": "input_text", "text": "</image>"}]}}`,
		`{"type": "event_msg", "payload": {"type": "task_started"}}`)
	turns, err := UserTurns(path)
	if err != nil || strings.Join(turns, "|") != "fix the build|see this" {
		t.Fatalf("got %q %v", turns, err)
	}
}

func TestCursorEventsAndTranscript(t *testing.T) {
	e := ParseEvent([]byte(`{"conversation_id": "c1", "session_id": "other", "hook_event_name": "stop",
		"transcript_path": "/t", "workspace_roots": ["/w"], "status": "completed", "loop_count": 0}`))
	if e.SessionID != "c1" || e.Cwd != "/w" || !e.incremental() {
		t.Fatalf("%+v", e)
	}
	if e := ParseEvent([]byte(`{"conversation_id": "c1", "hook_event_name": "preCompact", "trigger": "auto"}`)); e.Reason != "compact-auto" {
		t.Fatalf("%+v", e)
	}

	path := writeTranscript(t,
		`{"role": "user", "message": {"content": [{"type": "text", "text": "<timestamp>Monday, Oct 19, 2026, 1:00 PM (UTC+2)</timestamp>\n<user_query>\nfix the build\n</user_query>"}]}}`,
		`{"role": "assistant", "message": {"content": [{"type": "text", "text": "done"}, {"type": "tool_use", "name": "x"}]}}`,
		`{"role": "user", "message": {"content": [{"type": "text", "text": "<dynamic_tools>list</dynamic_tools>"}]}}`,
		`{"role": "user", "message": {"content": [{"type": "text", "text": "<user_query>ship it</user_query>"}]}}`)
	turns, err := UserTurns(path)
	if err != nil || strings.Join(turns, "|") != "fix the build|ship it" {
		t.Fatalf("got %q %v", turns, err)
	}
}
