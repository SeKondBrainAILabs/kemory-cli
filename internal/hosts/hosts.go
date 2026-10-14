// Package hosts knows where MCP hosts (Claude Code, Claude Desktop, Cursor,
// Continue, Warp) keep their config, whether each is installed, and how to add
// or remove the kemory server entry.
//
// Entries hold only the command that starts the bridge (`kemory --env <env>
// mcp serve`). No token or key is ever written into a host's config.
package hosts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/jsonedit"
)

// Names lists the supported hosts in display order.
var Names = []string{"claude-code", "claude-desktop", "cursor", "continue", "warp"}

// IsKnown reports whether name is a supported host.
func IsKnown(name string) bool {
	for _, n := range Names {
		if n == name {
			return true
		}
	}
	return false
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// ConfigPaths returns the candidate config files for host, most likely first.
// The first existing one is used; when none exists the first is created.
func ConfigPaths(host string) []string {
	h := home()
	switch host {
	case "claude-code":
		return []string{filepath.Join(h, ".claude.json")}
	case "claude-desktop":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(h, "AppData", "Roaming")
		}
		return []string{
			filepath.Join(h, "Library", "Application Support", "Claude", "claude_desktop_config.json"),
			filepath.Join(appData, "Claude", "claude_desktop_config.json"),
			filepath.Join(h, ".config", "Claude", "claude_desktop_config.json"),
		}
	case "cursor":
		return []string{filepath.Join(h, ".cursor", "mcp.json")}
	case "continue":
		return []string{filepath.Join(h, ".continue", "config.json")}
	case "warp":
		return []string{filepath.Join(h, ".warp", ".mcp.json")}
	}
	return nil
}

// appDirs are directories the host app itself creates. Claude Code's config
// sits in $HOME, so its app dir is ~/.claude.
func appDirs(host string) []string {
	if host == "claude-code" {
		return []string{filepath.Join(home(), ".claude")}
	}
	var dirs []string
	for _, p := range ConfigPaths(host) {
		dirs = append(dirs, filepath.Dir(p))
	}
	return dirs
}

// Installed reports whether the host app has left files of its own here. The
// config file alone does not count: `mcp install` may have created it.
func Installed(host string) bool {
	ours := map[string]bool{}
	for _, p := range ConfigPaths(host) {
		ours[filepath.Base(p)] = true
	}
	for _, dir := range appDirs(host) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !ours[e.Name()] {
				return true
			}
		}
	}
	return false
}

// ResolveConfig returns the config file to use for host, or "" for an
// unknown host.
func ResolveConfig(host string) string {
	paths := ConfigPaths(host)
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// KemoryEntries returns the names of kemory servers in a config file, or nil
// when it is missing or unreadable.
func KemoryEntries(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	cfg, err := jsonedit.Parse(data)
	if err != nil {
		return nil
	}
	servers, err := cfg.Object("mcpServers")
	if err != nil {
		return nil
	}
	var names []string
	for _, k := range servers.Keys() {
		if strings.Contains(k, "kemory") {
			names = append(names, k)
		}
	}
	return names
}

// Launch is how a host should start the bridge.
type Launch struct {
	Command string
	Args    []string
}

// LaunchFor returns the bridge command for env. A run through npx or uvx
// leaves no lasting `kemory` on PATH, so the host must launch the same way.
func LaunchFor(env string) Launch {
	args := []string{"--env", env, "mcp", "serve"}
	if os.Getenv("KEMORY_LAUNCHER") == "npx" {
		return Launch{Command: "npx", Args: append([]string{"-y", "kemory"}, args...)}
	}
	if runningUnderUVX() {
		return Launch{Command: "uvx", Args: append([]string{"kemory"}, args...)}
	}
	return Launch{Command: "kemory", Args: args}
}

// runningUnderUVX reports an ephemeral `uvx kemory ...` run: uv sets $UV, and
// the binary lives in uv's archive cache, which `uv cache clean` deletes.
func runningUnderUVX() bool {
	if os.Getenv("UV") == "" {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(exe), "/") {
		if strings.HasPrefix(part, "archive-v") {
			return true
		}
	}
	return false
}

// ErrInvalidConfig means a host config exists but is not a JSON object; it is
// never overwritten.
var ErrInvalidConfig = errors.New("not valid JSON")

// WriteEntry adds or replaces the server entry name in the config at path,
// leaving everything else in the file as it was.
func WriteEntry(path, name string, launch Launch) error {
	cfg := jsonedit.NewObject()
	mode := fs.FileMode(0o644)
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if cfg, err = jsonedit.Parse(data); err != nil {
			return fmt.Errorf("%s is %w. Refusing to overwrite — fix it first", path, ErrInvalidConfig)
		}
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // the host app's own config dir, normally readable
			return err
		}
	default:
		return err
	}
	servers, err := cfg.Object("mcpServers")
	if err != nil {
		return fmt.Errorf("%s: %w. Refusing to overwrite — fix it first", path, ErrInvalidConfig)
	}
	// A struct, not a map, so the entry reads command, args, env.
	entry := struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}{launch.Command, launch.Args, map[string]string{}}
	if err := servers.SetValue(name, entry); err != nil {
		return err
	}
	if err := cfg.SetObject("mcpServers", servers); err != nil {
		return err
	}
	return writeConfig(path, cfg, mode)
}

// RemoveEntries drops the named entries. It deletes the file (and the app dir
// when empty) only if nothing else was in it, i.e. only what kemory created.
func RemoveEntries(host, path string, names []string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	cfg, err := jsonedit.Parse(data)
	if err != nil {
		return fmt.Errorf("%s is %w", path, ErrInvalidConfig)
	}
	servers, err := cfg.Object("mcpServers")
	if err != nil {
		return err
	}
	for _, n := range names {
		servers.Delete(n)
	}
	if servers.Len() == 0 && (cfg.Len() == 0 || (cfg.Len() == 1 && cfg.Keys()[0] == "mcpServers")) {
		if err := os.Remove(path); err != nil {
			return err
		}
		dir := filepath.Dir(path)
		for _, d := range appDirs(host) {
			if d == dir && dir != home() {
				_ = os.Remove(dir) // fails, harmlessly, when other files remain
			}
		}
		return nil
	}
	if err := cfg.SetObject("mcpServers", servers); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return writeConfig(path, cfg, info.Mode().Perm())
}

// writeConfig replaces path atomically: the host app may read it at any time.
func writeConfig(path string, cfg *jsonedit.Object, mode fs.FileMode) error {
	body, err := cfg.Marshal("")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// ClaudeCodePluginSettings returns ~/.claude/settings.json when it enables the
// Kemory plugin, which bundles its own MCP server, or "".
func ClaudeCodePluginSettings() string {
	path := filepath.Join(home(), ".claude", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var settings struct {
		EnabledPlugins map[string]any `json:"enabledPlugins"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return ""
	}
	for k, v := range settings.EnabledPlugins {
		if enabled, _ := v.(bool); enabled && strings.HasPrefix(k, "kemory@") {
			return path
		}
	}
	return ""
}
