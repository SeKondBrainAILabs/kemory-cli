package hosts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codexEvents(t *testing.T, path string) map[string][]codexGroup {
	t.Helper()
	var file struct {
		Hooks map[string][]codexGroup `json:"hooks"`
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &file) != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return file.Hooks
}

func TestCodexHooksLeaveTheUsersOwnHooksAlone(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".codex", "hooks.json")
	mustWrite(t, path, `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "'/Users/me/.codex/hooks/mine.sh'", "timeout": 15}]}],
		"PreToolUse": [{"matcher": "shell", "hooks": [{"type": "command", "command": "audit.sh"}]}]}, "other": true}`)

	for range 2 { // the second run must change nothing
		if _, err := InstallCodexHooks(LaunchFor("prod")); err != nil {
			t.Fatal(err)
		}
	}
	events := codexEvents(t, path)
	if len(events["SessionStart"]) != 2 || events["SessionStart"][0].Hooks[0].Command != "'/Users/me/.codex/hooks/mine.sh'" {
		t.Fatalf("SessionStart: %+v", events["SessionStart"])
	}
	if got := events["SessionStart"][1].Hooks[0]; got.Command != "kemory --env prod hook session-start --agent codex" || got.Timeout != 10 {
		t.Fatalf("kemory's SessionStart hook: %+v", got)
	}
	if len(events["PreToolUse"]) != 1 || events["PreToolUse"][0].Matcher != "shell" {
		t.Fatalf("PreToolUse changed: %+v", events["PreToolUse"])
	}
	for _, e := range []string{"Stop", "PreCompact", "SessionEnd"} {
		if len(events[e]) != 1 || events[e][0].Hooks[0].Command != "kemory --env prod hook capture --agent codex" {
			t.Fatalf("%s: %+v", e, events[e])
		}
	}
	if events["SessionEnd"][0].Hooks[0].Timeout != 3 {
		t.Fatal("Codex caps SessionEnd at 3 seconds")
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"other": true`) {
		t.Fatal("other settings in the file were lost")
	}
	if !CodexHooksInstalled() {
		t.Fatal("installed hooks not detected")
	}
}

func TestCodexHooksRunTheWayKemoryWasStarted(t *testing.T) {
	setHome(t)
	t.Setenv("KEMORY_LAUNCHER", "npx")
	if got := codexHookCommand(LaunchFor("india"), "capture"); got != "npx -y kemory --env india hook capture --agent codex" {
		t.Fatalf("got %q", got)
	}
	if got := codexHookCommand(Launch{Command: "/Applications/My Tools/kemory", Args: []string{"mcp", "serve"}}, "capture"); got != "'/Applications/My Tools/kemory' hook capture --agent codex" {
		t.Fatalf("a path with a space must be quoted: %q", got)
	}
}

func TestCodexHooksRefuseAnInvalidFile(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".codex", "hooks.json")
	mustWrite(t, path, "{ not json")
	if _, err := InstallCodexHooks(LaunchFor("prod")); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "{ not json" {
		t.Fatal("an invalid file was overwritten")
	}
	if CodexHooksInstalled() {
		t.Fatal("an invalid file has no hooks")
	}
}
