package hosts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))
	t.Setenv("KEMORY_LAUNCHER", "")
	t.Setenv("UV", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("CODEX_HOME", "")
	return dir
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func servers(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.MCPServers
}

func TestAConfigWeCreatedDoesNotMakeAHostInstalled(t *testing.T) {
	home := setHome(t)
	mustWrite(t, filepath.Join(home, ".warp", ".mcp.json"), "{}")
	if Installed("warp") {
		t.Fatal("our own config counted as an installed app")
	}
	mustWrite(t, filepath.Join(home, ".warp", "settings.yaml"), "x")
	if !Installed("warp") {
		t.Fatal("an app file was not detected")
	}
	mustWrite(t, filepath.Join(home, ".claude.json"), "{}")
	if Installed("claude-code") {
		t.Fatal("claude-code is detected by ~/.claude, not by ~/.claude.json")
	}
}

func TestWriteEntryCreatesTheFileWithTheBridgeCommand(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".cursor", "mcp.json")
	if err := WriteEntry("cursor", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	got := servers(t, path)["kemory"]
	if got["command"] != "kemory" || strings.Join(toStrings(got["args"]), " ") != "--env prod mcp serve" {
		t.Fatalf("entry: %v", got)
	}
}

func TestWriteEntryLeavesTheUsersConfigAlone(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".claude.json")
	original := "{\n  \"numStartups\": 12,\n  \"shell\": \"a && b\",\n  \"mcpServers\": {\n    \"other\": {\"command\": \"x\"}\n  }\n}\n"
	mustWrite(t, path, original)
	if err := WriteEntry("claude-code", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if err := WriteEntry("claude-code", path, "kemory", LaunchFor("prod")); err != nil { // idempotent
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, `"shell": "a && b"`) || strings.Index(s, "numStartups") > strings.Index(s, "shell") {
		t.Fatalf("user content changed:\n%s", s)
	}
	srv := servers(t, path)
	if len(srv) != 2 || srv["other"]["command"] != "x" {
		t.Fatalf("servers: %v", srv)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Fatalf("file mode changed to %v", info.Mode().Perm())
		}
	}
}

func TestInvalidConfigIsRefusedAndUntouched(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".cursor", "mcp.json")
	mustWrite(t, path, "{ not json ]")
	err := WriteEntry("cursor", path, "kemory", LaunchFor("prod"))
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("got %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "{ not json ]" {
		t.Fatal("invalid config was overwritten")
	}
}

func TestLaunchUnderNPX(t *testing.T) {
	setHome(t)
	t.Setenv("KEMORY_LAUNCHER", "npx")
	l := LaunchFor("india")
	if l.Command != "npx" || strings.Join(l.Args, " ") != "-y kemory --env india mcp serve" {
		t.Fatalf("launch: %+v", l)
	}
}

func TestRemoveEntriesDeletesOnlyWhatKemoryCreated(t *testing.T) {
	home := setHome(t)
	ours := filepath.Join(home, ".warp", ".mcp.json")
	if err := WriteEntry("warp", ours, "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if err := RemoveEntries("warp", ours, []string{"kemory"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".warp")); !os.IsNotExist(err) {
		t.Fatal("the file and dir kemory created should be gone")
	}

	shared := filepath.Join(home, ".cursor", "mcp.json")
	mustWrite(t, shared, `{"theme": "dark", "mcpServers": {"kemory": {}, "other": {}}}`)
	if err := RemoveEntries("cursor", shared, []string{"kemory"}); err != nil {
		t.Fatal(err)
	}
	if srv := servers(t, shared); len(srv) != 1 || srv["other"] == nil {
		t.Fatalf("servers: %v", srv)
	}
}

func TestKemoryEntriesAndPluginDetection(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".claude.json")
	mustWrite(t, path, `{"mcpServers": {"kemory": {}, "kemory-india": {}, "github": {}}}`)
	if got := KemoryEntries("claude-code", path); strings.Join(got, ",") != "kemory,kemory-india" {
		t.Fatalf("entries: %v", got)
	}
	if ClaudeCodePluginSettings() != "" {
		t.Fatal("no settings file, no plugin")
	}
	mustWrite(t, filepath.Join(home, ".claude", "settings.json"), `{"enabledPlugins": {"kemory@kemory": true}}`)
	if ClaudeCodePluginSettings() == "" {
		t.Fatal("enabled plugin not detected")
	}
}

func TestResolveConfigPrefersAnExistingCandidate(t *testing.T) {
	home := setHome(t)
	win := filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
	mustWrite(t, win, "{}")
	if got := ResolveConfig("claude-desktop"); got != win {
		t.Fatalf("got %s", got)
	}
	if ResolveConfig("nope") != "" {
		t.Fatal("unknown host resolved")
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestOnlyContinueIsDiscontinued(t *testing.T) {
	for _, name := range Names {
		if Discontinued(name) != (name == "continue") {
			t.Fatalf("%s: discontinued = %v", name, Discontinued(name))
		}
	}
}

func TestFilesAnOlderKemoryLeftDoNotMakeAnAppInstalled(t *testing.T) {
	home := setHome(t)
	kemoryOnly := `{"mcpServers": {"kemory": {"command": "kemory", "args": ["mcp", "serve"]}}}`
	mustWrite(t, filepath.Join(home, ".continue", "config.json"), kemoryOnly)
	mustWrite(t, filepath.Join(home, ".continue", "config.json.bak-20260616"), kemoryOnly)
	if Installed("continue") {
		t.Fatal("files holding only kemory entries are not the app")
	}
	mustWrite(t, filepath.Join(home, ".continue", "config.json"),
		`{"mcpServers": {"kemory": {"command": "kemory"}, "github": {"command": "gh"}}}`)
	if !Installed("continue") {
		t.Fatal("a config with the user's own servers is the app's")
	}
}
