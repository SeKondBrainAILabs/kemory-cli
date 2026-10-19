package hosts

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// OpenCode loads every file in its plugins directory at startup. Kemory's
// plugin there loads the memory summary into the system prompt and saves the
// session's prompts, through `kemory hook`. OpenCode asks no approval for it.

//go:embed opencode.js
var openCodePlugin string

// commandPlaceholder is replaced with the command that starts kemory.
const commandPlaceholder = "__KEMORY_COMMAND__"

// openCodeDir is OpenCode's config directory: $XDG_CONFIG_HOME/opencode or
// ~/.config/opencode, on every OS.
func openCodeDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "opencode")
	}
	return filepath.Join(home(), ".config", "opencode")
}

// OpenCodePluginPath is where kemory's OpenCode plugin lives.
func OpenCodePluginPath() string {
	return filepath.Join(openCodeDir(), "plugins", "kemory.js")
}

// openCodePluginFor is the plugin source that starts kemory the way launch
// does.
func openCodePluginFor(launch Launch) (string, error) {
	command := []string{launch.Command}
	args := launch.Args
	if n := len(args); n >= 2 && args[n-2] == "mcp" && args[n-1] == "serve" {
		args = args[:n-2]
	}
	command = append(command, args...)
	quoted, err := json.Marshal(command)
	if err != nil {
		return "", err
	}
	return strings.Replace(openCodePlugin, commandPlaceholder, string(quoted), 1), nil
}

// InstallOpenCodePlugin writes kemory's plugin into OpenCode's plugins
// directory, replacing the one an earlier connect wrote.
func InstallOpenCodePlugin(launch Launch) (string, error) {
	path := OpenCodePluginPath()
	source, err := openCodePluginFor(launch)
	if err != nil {
		return path, err
	}
	return path, writeFile(path, []byte(source))
}

// OpenCodePluginInstalled reports whether kemory's OpenCode plugin is there.
func OpenCodePluginInstalled() bool {
	data, err := os.ReadFile(OpenCodePluginPath())
	return err == nil && strings.Contains(string(data), "export const KemoryMemory")
}
