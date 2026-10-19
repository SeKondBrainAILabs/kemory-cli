// Package hosts knows where MCP hosts (Claude Code, Codex, VS Code, Cursor and
// the rest of Names) keep their config, whether each is installed, and how to
// add or remove the kemory server entry.
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
	"runtime"
	"slices"
	"strings"
)

type host struct {
	name string
	// toml hosts keep each server as a [mcp_servers.<name>] table. The others
	// keep a JSON object of servers under key.
	toml bool
	key  string
	// typed hosts require "type": "stdio" in each entry.
	typed bool
	// local hosts (OpenCode) take {"type": "local", "command": [...]}.
	local bool
	// discontinued hosts are no longer offered, only kept working where
	// they are installed.
	discontinued bool
	// paths returns the candidate config files, most likely first.
	paths func() []string
	// appDirs are directories the host app itself creates. Nil means the
	// directories of the config files.
	appDirs func() []string
}

// inHome returns the path under $HOME as a one-element list.
func inHome(parts ...string) func() []string {
	return func() []string { return []string{filepath.Join(append([]string{home()}, parts...)...)} }
}

// registry is in display order.
var registry = []host{
	{
		name: "claude-code", key: "mcpServers",
		paths: inHome(".claude.json"), appDirs: inHome(".claude"),
	},
	{
		name: "claude-desktop", key: "mcpServers",
		paths: func() []string {
			return []string{
				filepath.Join(home(), "Library", "Application Support", "Claude", "claude_desktop_config.json"),
				filepath.Join(appData(), "Claude", "claude_desktop_config.json"),
				filepath.Join(home(), ".config", "Claude", "claude_desktop_config.json"),
			}
		},
	},
	{
		name: "codex", toml: true,
		paths: func() []string {
			dir := os.Getenv("CODEX_HOME")
			if dir == "" {
				dir = filepath.Join(home(), ".codex")
			}
			return []string{filepath.Join(dir, "config.toml")}
		},
	},
	{name: "cursor", key: "mcpServers", paths: inHome(".cursor", "mcp.json")},
	{
		name: "vscode", key: "servers", typed: true,
		paths: func() []string { return []string{filepath.Join(vscodeUserDir(), "mcp.json")} },
	},
	{name: "gemini-cli", key: "mcpServers", paths: inHome(".gemini", "settings.json")},
	{name: "antigravity", key: "mcpServers", paths: inHome(".gemini", "config", "mcp_config.json")},
	{
		// Windsurf is now Devin Desktop, which documents its own config dir.
		// Installs from before the rename keep the old one.
		name: "windsurf", key: "mcpServers",
		paths: func() []string {
			return []string{
				filepath.Join(userConfigDir(), "devin", "mcp_config.json"),
				filepath.Join(home(), ".codeium", "windsurf", "mcp_config.json"),
			}
		},
	},
	{
		// The VS Code extension first, then the Cline CLI.
		name: "cline", key: "mcpServers",
		paths: func() []string {
			return []string{
				filepath.Join(clineExtensionDir(), "settings", "cline_mcp_settings.json"),
				filepath.Join(home(), ".cline", "data", "settings", "cline_mcp_settings.json"),
			}
		},
		appDirs: func() []string { return []string{clineExtensionDir(), filepath.Join(home(), ".cline")} },
	},
	{
		name: "kiro", key: "mcpServers",
		paths: inHome(".kiro", "settings", "mcp.json"), appDirs: inHome(".kiro"),
	},
	{name: "grok", toml: true, paths: inHome(".grok", "config.toml")},
	{
		// Continue reads Claude Desktop-style JSON files in ~/.continue/mcpServers
		// whether the user's config is config.yaml or the older config.json, so
		// kemory owns a file there instead of editing either.
		// Continue was discontinued in 2026 after Cursor acquired it.
		name: "continue", key: "mcpServers", discontinued: true,
		paths: inHome(".continue", "mcpServers", "kemory.json"), appDirs: inHome(".continue"),
	},
	{name: "warp", key: "mcpServers", paths: inHome(".warp", ".mcp.json")},
	{
		name: "opencode", key: "mcp", local: true,
		paths: func() []string {
			return []string{filepath.Join(openCodeDir(), "opencode.json"), filepath.Join(openCodeDir(), "opencode.jsonc")}
		},
		appDirs: func() []string { return []string{openCodeDir(), filepath.Join(home(), ".local", "share", "opencode")} },
	},
}

// Discontinued reports whether host is a product that no longer exists, so
// kemory only keeps it working where it is installed.
func Discontinued(name string) bool {
	h, ok := lookup(name)
	return ok && h.discontinued
}

// Names lists the supported hosts in display order.
var Names = func() []string {
	names := make([]string, len(registry))
	for i, h := range registry {
		names[i] = h.name
	}
	return names
}()

func lookup(name string) (host, bool) {
	for _, h := range registry {
		if h.name == name {
			return h, true
		}
	}
	return host{}, false
}

// IsKnown reports whether name is a supported host.
func IsKnown(name string) bool {
	_, ok := lookup(name)
	return ok
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

func appData() string {
	if dir := os.Getenv("APPDATA"); dir != "" {
		return dir
	}
	return filepath.Join(home(), "AppData", "Roaming")
}

// userConfigDir is where apps that follow the platform convention keep their
// settings: %APPDATA% on Windows, $XDG_CONFIG_HOME or ~/.config elsewhere.
func userConfigDir() string {
	if runtime.GOOS == "windows" {
		return appData()
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(home(), ".config")
}

// vscodeUserDir is VS Code's user profile directory.
func vscodeUserDir() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home(), "Library", "Application Support", "Code", "User")
	}
	return filepath.Join(userConfigDir(), "Code", "User")
}

// clineExtensionDir is the Cline VS Code extension's storage directory.
func clineExtensionDir() string {
	return filepath.Join(vscodeUserDir(), "globalStorage", "saoudrizwan.claude-dev")
}

// ConfigPaths returns the candidate config files for host, most likely first.
func ConfigPaths(name string) []string {
	h, ok := lookup(name)
	if !ok {
		return nil
	}
	return h.paths()
}

func (h host) dirs() []string {
	if h.appDirs != nil {
		return h.appDirs()
	}
	var dirs []string
	for _, p := range h.paths() {
		dirs = append(dirs, filepath.Dir(p))
	}
	return dirs
}

// Installed reports whether the host app has left files of its own. Config
// files kemory may have created do not count, nor do directories holding only
// those: `mcp install` may have made them.
func Installed(name string) bool {
	h, ok := lookup(name)
	if !ok {
		return false
	}
	ours := map[string]bool{
		filepath.Clean(CodexHooksPath()):     true,
		filepath.Clean(CursorHooksPath()):    true,
		filepath.Clean(OpenCodePluginPath()): true,
	}
	for _, other := range registry {
		for _, p := range other.paths() {
			ours[filepath.Clean(p)] = true
		}
	}
	for _, dir := range h.dirs() {
		if hasAppFiles(filepath.Clean(dir), ours) {
			return true
		}
	}
	return false
}

func hasAppFiles(dir string, ours map[string]bool) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		switch {
		case ours[p]:
		case e.IsDir() && holdsAny(p, ours):
			if hasAppFiles(p, ours) {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// holdsAny reports whether dir is an ancestor of one of paths.
func holdsAny(dir string, paths map[string]bool) bool {
	prefix := dir + string(filepath.Separator)
	for p := range paths {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// ResolveConfig returns the config location to use for host: the first
// candidate that exists, else the first whose directory exists, else the first.
// It returns "" for an unknown host.
func ResolveConfig(name string) string {
	paths := ConfigPaths(name)
	if len(paths) == 0 {
		return ""
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, p := range paths {
		if info, err := os.Stat(filepath.Dir(p)); err == nil && info.IsDir() {
			return p
		}
	}
	return paths[0]
}

// KemoryEntries returns the names of kemory servers in host's config at path,
// or nil when it is missing or unreadable.
func KemoryEntries(name, path string) []string {
	h, ok := lookup(name)
	if !ok {
		return nil
	}
	names := jsonServerNames(path, h.key)
	if h.toml {
		names = tomlServerNames(path)
	}
	var kemory []string
	for _, n := range names {
		if strings.Contains(n, "kemory") {
			kemory = append(kemory, n)
		}
	}
	return kemory
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

// ErrInvalidConfig means a host config exists but cannot be parsed, or holds
// the servers in a shape kemory cannot edit safely. Such a file is never
// overwritten.
var ErrInvalidConfig = errors.New("refusing to change it — fix it first")

func invalid(path, problem string) error {
	return fmt.Errorf("%s %s: %w", path, problem, ErrInvalidConfig)
}

// WriteEntry adds or replaces the server entry name in host's config at path,
// leaving everything else in the file as it was.
func WriteEntry(name, path, server string, launch Launch) error {
	h, ok := lookup(name)
	if !ok {
		return fmt.Errorf("unknown host %q", name)
	}
	if h.toml {
		return writeTOMLEntry(path, server, launch)
	}
	return writeJSONEntry(path, h, server, launch)
}

// RemoveEntries drops the named entries from host's config at path. A file
// left with nothing else in it is deleted, with the directories that held only
// it, so that only what kemory created disappears.
func RemoveEntries(name, path string, servers []string) error {
	h, ok := lookup(name)
	if !ok {
		return fmt.Errorf("unknown host %q", name)
	}
	var emptied string
	var err error
	if h.toml {
		emptied, err = removeTOMLEntries(path, servers)
	} else {
		emptied, err = removeJSONEntries(path, h.key, servers)
	}
	if err != nil || emptied == "" {
		return err
	}
	if err := os.Remove(emptied); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	removeEmptyDirs(filepath.Dir(emptied), h.dirs())
	return nil
}

// removeEmptyDirs deletes dir and its empty parents, up to and including the
// app dir that holds it. It never goes above an app dir or touches $HOME.
func removeEmptyDirs(dir string, appDirs []string) {
	for _, top := range appDirs {
		top = filepath.Clean(top)
		if dir != top && !strings.HasPrefix(dir, top+string(filepath.Separator)) {
			continue
		}
		for d := dir; d != home(); d = filepath.Dir(d) {
			if os.Remove(d) != nil { // fails, harmlessly, when other files remain
				return
			}
			if d == top {
				return
			}
		}
		return
	}
}

// writeFile replaces path atomically, since the host app may read it at any
// time. It keeps an existing file's mode.
func writeFile(path string, body []byte) error {
	mode := fs.FileMode(0o644)
	switch info, err := os.Stat(path); {
	case err == nil:
		mode = info.Mode().Perm()
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // the host app's own config dir, normally readable
			return err
		}
	default:
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(body); err != nil {
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

func sorted(keys []string) []string {
	slices.Sort(keys)
	return keys
}
