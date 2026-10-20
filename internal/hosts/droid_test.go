package hosts

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDroidHooksAreKeyedByEventAtTheRoot(t *testing.T) {
	home := setHome(t)
	writeTestFile(t, filepath.Join(home, ".factory", "hooks.json"),
		`{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": "audit.sh"}]}]}`)
	path, err := InstallDroidHooks(LaunchFor("prod"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string][]hookGroup
	if err := json.Unmarshal([]byte(read(t, path)), &cfg); err != nil {
		t.Fatalf("%v\n%s", err, read(t, path))
	}
	if len(cfg["PreToolUse"]) != 1 || cfg["PreToolUse"][0].Hooks[0].Command != "audit.sh" {
		t.Fatalf("the user's hook changed:\n%s", read(t, path))
	}
	if got := cfg["SessionStart"]; len(got) != 1 || !strings.Contains(got[0].Hooks[0].Command, "hook session-start --agent droid") {
		t.Fatalf("no SessionStart hook:\n%s", read(t, path))
	}
	if !DroidHooksInstalled() {
		t.Fatal("not reported installed")
	}
	if strings.Contains(read(t, path), `"hooks": {`) {
		t.Fatalf("hooks.json must not hold a hooks object:\n%s", read(t, path))
	}
}

func TestDroidHooksStayInSettingsWhereTheUserKeepsThem(t *testing.T) {
	home := setHome(t)
	settings := filepath.Join(home, ".factory", "settings.json")
	writeTestFile(t, settings, `{"model": "x", "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "notify.sh"}]}]}}`)
	path, err := InstallDroidHooks(LaunchFor("prod"))
	if err != nil {
		t.Fatal(err)
	}
	if path != settings {
		t.Fatalf("wrote %s; a new hooks.json would switch the user's hooks off", path)
	}
	got := read(t, settings)
	if !strings.Contains(got, "notify.sh") || !strings.Contains(got, "hook capture --agent droid") || !strings.Contains(got, `"model": "x"`) {
		t.Fatalf("got:\n%s", got)
	}
}
