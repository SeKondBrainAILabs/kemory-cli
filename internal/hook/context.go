package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Instruction leads every injected context. It says what no hook can: what
// Kemory is and the standard for writing to it. Same text as the plugin's
// session-start.sh.
const Instruction = `Kemory is this user's persistent memory, shared across their AIs and sessions.
- Check it before answering anything about their work, their decisions, or
  past sessions, and again when the topic shifts.
- Write the moment they state a preference, a decision is reached, or you
  learn something non-obvious. Do not ask whether to save it — save it, then
  say in one line what you stored and where.
- Where: personal ways of working in a user-scoped namespace, project facts
  in a shared one. Call kemory_list_namespaces and reuse what is already
  there rather than inventing a near-duplicate.
- Write in the words the thing would be searched for later: identifiers,
  error strings and names you actually used, not a paraphrase.`

const consolidate = "This session was just compacted. Anything established before the " +
	"compaction now exists only as a summary. If this session produced " +
	"durable facts, decisions or solved problems that are not in kemory " +
	"yet, store them now with kemory_consolidate_session (or " +
	"kemory_store_memory for individual facts) before continuing."

const trailer = "Recall details with kemory_recall_memory / kemory_get_context before " +
	"re-deriving anything, and rate what you use with kemory_rate_memory."

// ContextPath is the request that returns the user's namespace summaries,
// at the depth KEMORY_CONTEXT_DEPTH names (default l3).
func ContextPath() string {
	depth := os.Getenv("KEMORY_CONTEXT_DEPTH")
	if depth == "" {
		depth = "l3"
	}
	return "/api/v1/user/context?depth=" + depth
}

// BuildContext turns a /api/v1/user/context response into the text a session
// starts with. compacted is true when the session resumes after compaction.
func BuildContext(body []byte, compacted bool) string {
	return strings.Join(nonEmpty(Instruction, summaries(body, compacted)), "\n\n")
}

func summaries(body []byte, compacted bool) string {
	nudge := ""
	if compacted {
		nudge = consolidate
	}
	var resp struct {
		Namespaces []struct {
			Namespace string `json:"namespace"`
			Summary   string `json:"summary"`
		} `json:"namespaces"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.Namespaces == nil {
		return nudge
	}
	wanted := map[string]bool{}
	for _, n := range strings.Split(os.Getenv("KEMORY_CONTEXT_NAMESPACES"), ",") {
		if n = strings.TrimSpace(n); n != "" {
			wanted[n] = true
		}
	}
	var lines []string
	for _, ns := range resp.Namespaces {
		s := strings.TrimSpace(ns.Summary)
		if s == "" || (len(wanted) > 0 && !wanted[ns.Namespace]) {
			continue
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", ns.Namespace, s))
	}
	if len(lines) == 0 {
		return nudge
	}
	budget := 4000
	if n, err := strconv.Atoi(os.Getenv("KEMORY_CONTEXT_MAX_CHARS")); err == nil {
		budget = max(500, n)
	}
	var kept []string
	used := 0
	for _, l := range lines {
		// Characters, not bytes, as the plugin counts them.
		n := len([]rune(l))
		if used+n > budget {
			break
		}
		kept = append(kept, l)
		used += n
	}
	if len(kept) == 0 {
		return nudge
	}
	text := "Your Kemory memory (persistent across sessions) — namespace summaries:\n" + strings.Join(kept, "\n")
	if omitted := len(lines) - len(kept); omitted > 0 {
		text += fmt.Sprintf("\n\n(%d more namespace summaries omitted to stay "+
			"within the context budget; raise KEMORY_CONTEXT_MAX_CHARS to include them.)", omitted)
	}
	if compacted {
		text += "\n\n" + consolidate
	}
	return text + "\n\n" + trailer
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Output wraps context in the shape agent injects at session start.
func Output(agent, context string) ([]byte, error) {
	switch agent {
	case "cursor":
		return json.Marshal(map[string]string{"additional_context": context})
	case "copilot":
		return json.Marshal(map[string]string{"additionalContext": context})
	case "claude-code", "codex", "gemini-cli":
		return json.Marshal(map[string]any{"hookSpecificOutput": map[string]string{
			"hookEventName":     "SessionStart",
			"additionalContext": context,
		}})
	}
	return []byte(context), nil
}
