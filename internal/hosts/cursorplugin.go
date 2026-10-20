package hosts

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Cursor runs every MCP server and hook it finds side by side, with no
// dedupe: its own plugins, the Claude Code plugins in ~/.claude, and the
// entries in ~/.cursor/mcp.json and hooks.json. A Kemory plugin from either
// source already gives Cursor the tools and the memory hooks.

// CursorPluginManifest returns the plugin.json of the Kemory Cursor plugin,
// installed locally or from a marketplace, or "".
func CursorPluginManifest() string {
	dir := filepath.Join(home(), ".cursor", "plugins")
	local, _ := filepath.Glob(filepath.Join(dir, "local", "*", ".cursor-plugin", "plugin.json"))
	for _, path := range local {
		if isKemoryManifest(path) {
			return path
		}
	}
	// A marketplace install is cached per version; Cursor marks a finished
	// download with .cache-complete.
	cached, _ := filepath.Glob(filepath.Join(dir, "cache", "*", "kemory", "*", ".cursor-plugin", "plugin.json"))
	for _, path := range cached {
		version := filepath.Dir(filepath.Dir(path))
		if _, err := os.Stat(filepath.Join(version, ".cache-complete")); err == nil && isKemoryManifest(path) {
			return path
		}
	}
	return ""
}

func isKemoryManifest(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var manifest struct {
		Name string `json:"name"`
	}
	return json.Unmarshal(data, &manifest) == nil && manifest.Name == "kemory"
}

// ClaudeCodePluginLoaded returns ~/.claude/settings.json when the Claude Code
// Kemory plugin is both installed and enabled, which is when Cursor loads it
// too, or "".
func ClaudeCodePluginLoaded() string {
	const plugin = "kemory@kemory"
	data, err := os.ReadFile(filepath.Join(home(), ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return ""
	}
	var installed struct {
		Plugins map[string][]json.RawMessage `json:"plugins"`
	}
	if json.Unmarshal(data, &installed) != nil || len(installed.Plugins[plugin]) == 0 {
		return ""
	}
	path := filepath.Join(home(), ".claude", "settings.json")
	if data, err = os.ReadFile(path); err != nil {
		return ""
	}
	var settings struct {
		EnabledPlugins map[string]any `json:"enabledPlugins"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return ""
	}
	if enabled, _ := settings.EnabledPlugins[plugin].(bool); !enabled {
		return ""
	}
	return path
}
