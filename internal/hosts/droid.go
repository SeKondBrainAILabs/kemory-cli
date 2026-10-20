package hosts

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Factory's Droid reads user hooks from ~/.factory/hooks.json, keyed by event
// at its root, and only when that file is absent from the "hooks" object in
// ~/.factory/settings.json. Creating hooks.json would switch hooks kept in
// settings.json off, so kemory writes to whichever of the two Droid reads.
// Its hooks match Claude Code's: grouped command handlers, and SessionStart
// adds the hook's output to the session.

func factoryDir() string { return filepath.Join(home(), ".factory") }

// DroidHooksPath is the hooks file Droid reads.
func DroidHooksPath() string {
	hooks := filepath.Join(factoryDir(), "hooks.json")
	if _, err := os.Stat(hooks); err == nil {
		return hooks
	}
	settings := filepath.Join(factoryDir(), "settings.json")
	if data, err := os.ReadFile(settings); err == nil {
		var cfg map[string]json.RawMessage
		if json.Unmarshal(data, &cfg) == nil && cfg["hooks"] != nil {
			return settings
		}
	}
	return hooks
}

var droidHooks = groupedHooks{
	agent: "droid", path: DroidHooksPath, timeoutScale: 1, topLevelFile: "hooks.json",
	events: []hookEvent{
		{"SessionStart", "session-start", 10},
		{"Stop", "capture", 10},
		{"PreCompact", "capture", 10},
		{"SessionEnd", "capture", 10},
	},
}

// InstallDroidHooks adds kemory's hooks to Droid's hooks file.
func InstallDroidHooks(launch Launch) (string, error) { return droidHooks.install(launch) }

// DroidHooksInstalled reports whether every kemory hook is in Droid's hooks
// file.
func DroidHooksInstalled() bool { return droidHooks.installed() }
