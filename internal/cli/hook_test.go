package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const contextReply = `{"namespaces": [{"namespace": "user:preferences", "summary": "Prefers Go."}]}`

func TestHookSessionStartPrintsTheAgentsShape(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){"/api/v1/user/context": jsonReply(200, contextReply)})

	withStdin(t, `{"source": "startup"}`)
	code, out, _ := run(t, "hook", "session-start", "--agent", "codex")
	var got struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || !strings.Contains(got.HookSpecificOutput.AdditionalContext, "- [user:preferences] Prefers Go.") {
		t.Fatalf("got %d %q", code, out)
	}

	withStdin(t, `{}`)
	if _, out, _ := run(t, "hook", "session-start", "--agent", "cursor"); !strings.Contains(out, `"additional_context"`) {
		t.Fatalf("cursor: %q", out)
	}
}

func TestHookSessionStartIsSilentWhenItCannotHelp(t *testing.T) {
	setHome(t)
	withStdin(t, `{}`)
	if code, out, errOut := run(t, "hook", "session-start"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("signed out: %d %q %q", code, out, errOut)
	}

	newFakeVault(t, map[string]func(http.ResponseWriter){"/api/v1/user/context": jsonReply(500, `{}`)})
	withStdin(t, `{}`)
	if code, out, _ := run(t, "hook", "session-start"); code != 0 || out != "" {
		t.Fatalf("server error: %d %q", code, out)
	}

	t.Setenv("KEMORY_CONTEXT", "0")
	newFakeVault(t, map[string]func(http.ResponseWriter){"/api/v1/user/context": jsonReply(200, contextReply)})
	withStdin(t, `{}`)
	if _, out, _ := run(t, "hook", "session-start"); out != "" {
		t.Fatalf("KEMORY_CONTEXT=0 must turn it off: %q", out)
	}
}

// hookEvent is a hook payload for session s1, marshalled so Windows paths
// stay valid JSON.
func hookEvent(t *testing.T, event, transcript string) string {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"session_id": "s1", "hook_event_name": event, "transcript_path": transcript, "cwd": "/w",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func captureSetup(t *testing.T, status int) (*fakeVault, string, string) {
	t.Helper()
	home := setHome(t)
	for _, k := range []string{"KEMORY_AUTO_CAPTURE", "KEMORY_CAPTURE_MIN_NEW_TURNS", "KEMORY_CAPTURE_MAX_TURNS", "KEMORY_CAPTURE_NAMESPACE", "KEMORY_CAPTURE_VISIBILITY"} {
		t.Setenv(k, "")
	}
	v := newFakeVault(t, map[string]func(http.ResponseWriter){"/api/v1/memories": jsonReply(status, `{"id": "m1"}`)})
	transcript := filepath.Join(home, "t.jsonl")
	writeFile(t, transcript, `{"type": "user", "message": {"content": "deploy with token=abcdef123456"}}`+"\n"+
		`{"type": "user", "message": {"content": "two"}}`+"\n"+`{"type": "user", "message": {"content": "three"}}`+"\n")
	return v, home, transcript
}

func TestHookCaptureStoresRedactedTurnsOnce(t *testing.T) {
	v, home, transcript := captureSetup(t, 201)
	withStdin(t, hookEvent(t, "Stop", transcript))
	if code, out, errOut := run(t, "hook", "capture", "--agent", "codex"); code != 0 || out != "" || errOut != "" {
		t.Fatalf("capture must print nothing: %d %q %q", code, out, errOut)
	}
	body := v.bodies["/api/v1/memories"]
	content, _ := body["content"].(string)
	if !strings.Contains(content, "- deploy with [REDACTED]") || strings.Contains(content, "abcdef123456") {
		t.Fatalf("content: %s", content)
	}
	if md, _ := body["metadata"].(map[string]any); md["source"] != "codex" {
		t.Fatalf("metadata: %v", body["metadata"])
	}
	if _, err := os.Stat(filepath.Join(home, ".kemory", ".captured", "s1")); err != nil {
		t.Fatal("the high-water mark was not saved")
	}

	delete(v.bodies, "/api/v1/memories")
	withStdin(t, hookEvent(t, "SessionEnd", transcript))
	run(t, "hook", "capture", "--agent", "codex")
	if _, posted := v.bodies["/api/v1/memories"]; posted {
		t.Fatal("the same session was stored twice")
	}
}

func TestHookCaptureFlagsFillAMissingPayload(t *testing.T) {
	v, _, transcript := captureSetup(t, 201)
	withStdin(t, "")
	run(t, "hook", "capture", "--agent", "text", "--transcript", transcript, "--session-id", "s9", "--event", "SessionEnd")
	if v.bodies["/api/v1/memories"]["session_id"] != "s9" {
		t.Fatalf("body: %v", v.bodies["/api/v1/memories"])
	}
}

func TestAFailedCaptureIsTriedAgain(t *testing.T) {
	v, home, transcript := captureSetup(t, 503)
	withStdin(t, hookEvent(t, "SessionEnd", transcript))
	if code, _, _ := run(t, "hook", "capture"); code != 0 {
		t.Fatalf("a failed store must not fail the agent: %d", code)
	}
	if _, tried := v.bodies["/api/v1/memories"]; !tried {
		t.Fatal("the store was never attempted")
	}
	if _, err := os.Stat(filepath.Join(home, ".kemory", ".captured", "s1")); !os.IsNotExist(err) {
		t.Fatal("the mark advanced past turns that were not stored")
	}
}

func TestHookCaptureCanBeTurnedOff(t *testing.T) {
	v, _, transcript := captureSetup(t, 201)
	t.Setenv("KEMORY_AUTO_CAPTURE", "0")
	withStdin(t, hookEvent(t, "SessionEnd", transcript))
	run(t, "hook", "capture")
	if _, posted := v.bodies["/api/v1/memories"]; posted {
		t.Fatal("KEMORY_AUTO_CAPTURE=0 must turn capture off")
	}
}

func TestCopilotSessionStartAnswersEachHarnessInItsShape(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){"/api/v1/user/context": jsonReply(200, contextReply)})

	// The Copilot CLI sends camelCase and reads a top-level additionalContext.
	withStdin(t, `{"sessionId": "c1", "timestamp": 1, "cwd": "/w", "source": "new"}`)
	_, out, _ := run(t, "hook", "session-start", "--agent", "copilot")
	var cli map[string]any
	if json.Unmarshal([]byte(out), &cli) != nil || !strings.Contains(cli["additionalContext"].(string), "Prefers Go.") {
		t.Fatalf("copilot cli: %q", out)
	}

	// VS Code sends Claude Code's payload and reads hookSpecificOutput.
	withStdin(t, `{"session_id": "v1", "hook_event_name": "SessionStart", "source": "new"}`)
	_, out, _ = run(t, "hook", "session-start", "--agent", "copilot")
	var vscode struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if json.Unmarshal([]byte(out), &vscode) != nil || vscode.HookSpecificOutput.HookEventName != "SessionStart" ||
		!strings.Contains(vscode.HookSpecificOutput.AdditionalContext, "Prefers Go.") {
		t.Fatalf("vscode: %q", out)
	}
}

func TestCopilotCLISessionEndFindsItsTranscript(t *testing.T) {
	v, home, _ := captureSetup(t, 201)
	events := filepath.Join(home, ".copilot", "session-state", "c7", "events.jsonl")
	writeFile(t, events, `{"type": "session.start", "data": {}}`+"\n"+
		`{"type": "user.message", "data": {"content": "fix the build", "attachments": []}, "id": "1"}`+"\n"+
		`{"type": "assistant.message", "data": {"content": "done"}}`+"\n")
	withStdin(t, `{"sessionId": "c7", "timestamp": 1, "cwd": "/w", "reason": "complete"}`)
	run(t, "hook", "capture", "--agent", "copilot", "--event", "SessionEnd")
	content, _ := v.bodies["/api/v1/memories"]["content"].(string)
	if !strings.Contains(content, "- fix the build") || strings.Contains(content, "done") {
		t.Fatalf("content: %q", content)
	}
}
