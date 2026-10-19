package hosts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cursorEvents(t *testing.T, path string) (int, map[string][]cursorHandler) {
	t.Helper()
	var file struct {
		Version int                        `json:"version"`
		Hooks   map[string][]cursorHandler `json:"hooks"`
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &file) != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return file.Version, file.Hooks
}

func TestCursorHooksLeaveTheUsersOwnHooksAlone(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".cursor", "hooks.json")
	mustWrite(t, path, `{"version": 1, "hooks": {"sessionStart": [{"command": "./hooks/mine.sh"}],
		"beforeShellExecution": [{"command": "audit.sh", "matcher": "rm"}]}}`)

	for range 2 { // the second run must change nothing
		if _, err := InstallCursorHooks(LaunchFor("prod")); err != nil {
			t.Fatal(err)
		}
	}
	_, events := cursorEvents(t, path)
	if len(events["sessionStart"]) != 2 || events["sessionStart"][0].Command != "./hooks/mine.sh" {
		t.Fatalf("sessionStart: %+v", events["sessionStart"])
	}
	if got := events["sessionStart"][1]; got.Command != "kemory --env prod hook session-start --agent cursor" || got.Timeout != 10 {
		t.Fatalf("kemory's sessionStart hook: %+v", got)
	}
	if len(events["beforeShellExecution"]) != 1 {
		t.Fatalf("beforeShellExecution changed: %+v", events["beforeShellExecution"])
	}
	for _, e := range []string{"stop", "preCompact", "sessionEnd"} {
		if len(events[e]) != 1 || events[e][0].Command != "kemory --env prod hook capture --agent cursor" {
			t.Fatalf("%s: %+v", e, events[e])
		}
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"matcher": "rm"`) {
		t.Fatal("the user's hook lost a field")
	}
	if !CursorHooksInstalled() {
		t.Fatal("installed hooks not detected")
	}
}

func TestCursorHooksStartANewFileAtVersion1(t *testing.T) {
	setHome(t)
	path, err := InstallCursorHooks(LaunchFor("prod"))
	if err != nil {
		t.Fatal(err)
	}
	if version, events := cursorEvents(t, path); version != 1 || len(events) != 4 {
		t.Fatalf("version %d, events %+v", version, events)
	}
}

func TestCursorHooksRefuseAnInvalidFile(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".cursor", "hooks.json")
	mustWrite(t, path, `{"version": 1, "hooks": {"stop": "not a list"}}`)
	if _, err := InstallCursorHooks(LaunchFor("prod")); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
	if CursorHooksInstalled() {
		t.Fatal("an invalid file has no hooks")
	}
}

func TestKemorysHookFilesDoNotMakeAnAppLookInstalled(t *testing.T) {
	setHome(t)
	if _, err := InstallCursorHooks(LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallCodexHooks(LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if Installed("cursor") || Installed("codex") {
		t.Fatal("a hooks file kemory wrote is not the app")
	}
}
