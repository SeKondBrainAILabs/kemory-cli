package hosts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codexEvents(t *testing.T, path string) map[string][]hookGroup {
	t.Helper()
	var file struct {
		Hooks map[string][]hookGroup `json:"hooks"`
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
	if got := hookCommand(LaunchFor("india"), "capture", "codex"); got != "npx -y kemory --env india hook capture --agent codex" {
		t.Fatalf("got %q", got)
	}
	if got := hookCommand(Launch{Command: "/Applications/My Tools/kemory", Args: []string{"mcp", "serve"}}, "capture", "codex"); got != "'/Applications/My Tools/kemory' hook capture --agent codex" {
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

func TestGeminiHooksShareTheSettingsFileWithItsMCPServers(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".gemini", "settings.json")
	mustWrite(t, path, `{"theme": "GitHub", "hooks": {"BeforeTool": [{"matcher": "write_file", "hooks": [{"type": "command", "command": "check.sh", "timeout": 5000}]}]}}`)
	if err := WriteEntry("gemini-cli", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	for range 2 { // the second run must change nothing
		if _, err := InstallGeminiHooks(LaunchFor("prod")); err != nil {
			t.Fatal(err)
		}
	}
	events := codexEvents(t, path)
	start := events["SessionStart"]
	if len(start) != 1 || start[0].Hooks[0].Command != "kemory --env prod hook session-start --agent gemini-cli" ||
		start[0].Hooks[0].Timeout != 10000 || start[0].Hooks[0].Name != "kemory-session-start" {
		t.Fatalf("SessionStart: %+v", start)
	}
	for _, e := range []string{"AfterAgent", "PreCompress", "SessionEnd"} {
		if len(events[e]) != 1 || events[e][0].Hooks[0].Command != "kemory --env prod hook capture --agent gemini-cli" {
			t.Fatalf("%s: %+v", e, events[e])
		}
	}
	if len(events["BeforeTool"]) != 1 {
		t.Fatalf("the user's hook changed: %+v", events["BeforeTool"])
	}
	data := read(t, path)
	if !strings.Contains(data, `"theme": "GitHub"`) || !strings.Contains(data, `"mcpServers"`) {
		t.Fatalf("settings lost:\n%s", data)
	}
	if !GeminiHooksInstalled() || CodexHooksInstalled() {
		t.Fatal("each agent's hooks are its own")
	}
}

func TestCopilotHooksFileServesVSCodeAndTheCLI(t *testing.T) {
	home := setHome(t)
	t.Setenv("COPILOT_HOME", "")
	path, err := InstallCopilotHooks(LaunchFor("prod"))
	if err != nil || path != filepath.Join(home, ".copilot", "hooks", "kemory.json") {
		t.Fatalf("%s %v", path, err)
	}
	var f copilotHooksFile
	if err := json.Unmarshal([]byte(read(t, path)), &f); err != nil || f.Version != 1 {
		t.Fatalf("%+v %v", f, err)
	}
	want := map[string]string{
		"sessionStart": "kemory --env prod hook session-start --agent copilot",
		"agentStop":    "kemory --env prod hook capture --agent copilot --event Stop",
		"preCompact":   "kemory --env prod hook capture --agent copilot --event PreCompact",
		"sessionEnd":   "kemory --env prod hook capture --agent copilot --event SessionEnd",
	}
	for event, command := range want {
		if h := f.Hooks[event]; len(h) != 1 || h[0].Command != command || h[0].TimeoutSec != 10 {
			t.Fatalf("%s: %+v", event, h)
		}
	}
	if !CopilotHooksInstalled() {
		t.Fatal("installed hooks not detected")
	}
	t.Setenv("COPILOT_HOME", filepath.Join(home, "elsewhere"))
	if CopilotHooksInstalled() || !strings.HasPrefix(CopilotHooksPath(), filepath.Join(home, "elsewhere")) {
		t.Fatal("COPILOT_HOME moves the hooks")
	}
}

func TestCodexPluginInstallFollowsCodexHome(t *testing.T) {
	home := setHome(t)
	codexHome := filepath.Join(home, "elsewhere")
	t.Setenv("CODEX_HOME", codexHome)
	if got := CodexPluginInstall(); got != "" {
		t.Fatalf("no plugin installed, got %q", got)
	}
	// The same file under ~/.codex does not count when CODEX_HOME is set.
	record := filepath.Join("plugins", "cache", "openai-curated-remote", "app-6a85afc169ac819185a36cd38c9fe432", ".codex-remote-plugin-install.json")
	mustWrite(t, filepath.Join(home, ".codex", record), `{"schema_version":1}`)
	if got := CodexPluginInstall(); got != "" {
		t.Fatalf("read the plugin from ~/.codex despite CODEX_HOME: %q", got)
	}
	want := filepath.Join(codexHome, record)
	mustWrite(t, want, `{"schema_version":1,"remote_plugin_id":"plugin_asdk_app_6a85afc169ac819185a36cd38c9fe432"}`)
	if got := CodexPluginInstall(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
