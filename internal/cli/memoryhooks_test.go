package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorAddsMissingCodexHooks(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	installApp(t, home, ".codex")
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), "[mcp_servers.kemory]\ncommand = \"kemory\"\nargs = [\"mcp\", \"serve\"]\n")

	_, report := runDoctor(t)
	if statusOf(report, "mcp host: codex") != "skip" {
		t.Fatalf("tools without hooks must be reported: %+v", report)
	}
	_, report = runDoctor(t, "--fix")
	if statusOf(report, "mcp host: codex") != "pass" {
		t.Fatalf("after --fix: %+v", report)
	}
	if !strings.Contains(readFile(t, filepath.Join(home, ".codex", "hooks.json")), "hook capture --agent codex") {
		t.Fatal("--fix did not add the hooks")
	}
	for _, c := range report.Checks {
		if c.Check == "mcp host: codex" && (c.Detail == nil || !strings.Contains(*c.Detail, "/hooks")) {
			t.Fatalf("the user must be told to approve the hooks: %+v", c)
		}
	}
}

func TestConnectCodexAddsTheEntryAndHooks(t *testing.T) {
	home := setHome(t)
	code, out, _ := run(t, "mcp", "install", "--host", "codex")
	if code != 0 || !strings.Contains(out, "approve them once with /hooks in Codex") {
		t.Fatalf("got %d %q", code, out)
	}
	if !strings.Contains(readFile(t, filepath.Join(home, ".codex", "config.toml")), "[mcp_servers.kemory]") {
		t.Fatal("no MCP entry")
	}
	if !strings.Contains(readFile(t, filepath.Join(home, ".codex", "hooks.json")), "hook session-start --agent codex") {
		t.Fatal("no hooks")
	}
}

func TestConnectCursorAddsTheEntryAndHooksWithNothingToApprove(t *testing.T) {
	home := setHome(t)
	code, out, _ := run(t, "mcp", "install", "--host", "cursor")
	if code != 0 || !strings.Contains(out, "automatic memory hooks in") || strings.Contains(out, "approve") {
		t.Fatalf("got %d %q", code, out)
	}
	if !strings.Contains(readFile(t, filepath.Join(home, ".cursor", "hooks.json")), "hook session-start --agent cursor") {
		t.Fatal("no hooks")
	}
	installApp(t, home, ".cursor")
	fakeReleases(t, `[]`)
	_, report := runDoctor(t)
	if statusOf(report, "mcp host: cursor") != "pass" {
		t.Fatalf("doctor: %+v", report)
	}
	for _, c := range report.Checks {
		if c.Check == "mcp host: cursor" && (c.Detail == nil || !strings.Contains(*c.Detail, "automatic memory hooks installed")) {
			t.Fatalf("doctor: %+v", c)
		}
	}
}

func TestConnectOpenCodeAddsTheEntryAndPlugin(t *testing.T) {
	home := setHome(t)
	code, out, _ := run(t, "mcp", "install", "--host", "opencode")
	if code != 0 || !strings.Contains(out, filepath.Join("opencode", "plugins", "kemory.js")) {
		t.Fatalf("got %d %q", code, out)
	}
	config := filepath.Join(home, ".config", "opencode", "opencode.json")
	if !strings.Contains(readFile(t, config), `"type": "local"`) {
		t.Fatal("no MCP entry")
	}
	installApp(t, home, filepath.Join(".config", "opencode"))
	fakeReleases(t, `[]`)
	if _, report := runDoctor(t); statusOf(report, "mcp host: opencode") != "pass" {
		t.Fatalf("doctor: %+v", report)
	}
}

func TestConnectGeminiCLIAddsTheEntryAndHooks(t *testing.T) {
	home := setHome(t)
	code, out, _ := run(t, "mcp", "install", "--host", "gemini-cli")
	if code != 0 || !strings.Contains(out, "automatic memory hooks in") || strings.Contains(out, "approve") {
		t.Fatalf("got %d %q", code, out)
	}
	settings := readFile(t, filepath.Join(home, ".gemini", "settings.json"))
	if !strings.Contains(settings, `"mcpServers"`) || !strings.Contains(settings, "hook session-start --agent gemini-cli") {
		t.Fatalf("settings:\n%s", settings)
	}
}

func TestConnectVSCodeAddsCopilotHooks(t *testing.T) {
	home := setHome(t)
	t.Setenv("COPILOT_HOME", "")
	code, out, _ := run(t, "mcp", "install", "--host", "vscode")
	if code != 0 || !strings.Contains(out, filepath.Join(".copilot", "hooks", "kemory.json")) {
		t.Fatalf("got %d %q", code, out)
	}
	if !strings.Contains(readFile(t, filepath.Join(home, ".copilot", "hooks", "kemory.json")), "--agent copilot") {
		t.Fatal("no hooks")
	}
}

// installCodexPlugin records the Kemory plugin from OpenAI's plugin directory
// the way Codex does, under a CODEX_HOME it sets, and returns that home.
func installCodexPlugin(t *testing.T, home string) string {
	t.Helper()
	codexHome := filepath.Join(home, "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	writeFile(t, filepath.Join(codexHome, "plugins", "cache", "openai-curated-remote", "app-6a85afc169ac819185a36cd38c9fe432",
		".codex-remote-plugin-install.json"), `{"schema_version":1,"remote_plugin_id":"plugin_asdk_app_6a85afc169ac819185a36cd38c9fe432"}`)
	return codexHome
}

func codexDetail(r doctorReport) string {
	for _, c := range r.Checks {
		if c.Check == "mcp host: codex" && c.Detail != nil {
			return *c.Detail
		}
	}
	return ""
}

func TestConnectCodexWithThePluginAddsOnlyTheHooks(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	codexHome := installCodexPlugin(t, home)
	config := filepath.Join(codexHome, "config.toml")

	code, out, _ := run(t, "mcp", "install", "--host", "codex")
	if code != 0 || !strings.Contains(out, "already connected through the Kemory plugin") || !strings.Contains(out, "/hooks") {
		t.Fatalf("got %d %q", code, out)
	}
	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatal("wrote an MCP entry next to the plugin's tools")
	}
	if !strings.Contains(readFile(t, filepath.Join(codexHome, "hooks.json")), "hook session-start --agent codex") {
		t.Fatal("no hooks")
	}
	_, report := runDoctor(t)
	if statusOf(report, "mcp host: codex") != "pass" || !strings.Contains(codexDetail(report), "Kemory plugin for tools, hooks from kemory-cli") {
		t.Fatalf("doctor: %+v", report)
	}

	// The plugin connects to prod; another env still gets its entry.
	if code, _, _ := run(t, "--env", "india", "mcp", "install", "--host", "codex"); code != 0 ||
		!strings.Contains(readFile(t, config), "[mcp_servers.kemory-india]") {
		t.Fatal("the india entry must still be written")
	}
}

func TestDoctorReportsCodexPluginWithoutHooks(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	codexHome := installCodexPlugin(t, home)

	_, report := runDoctor(t)
	if statusOf(report, "mcp host: codex") != "skip" || !strings.Contains(codexDetail(report), "no automatic memory") {
		t.Fatalf("missing hooks must be reported: %+v", report)
	}
	_, report = runDoctor(t, "--fix")
	if statusOf(report, "mcp host: codex") != "pass" || !strings.Contains(readFile(t, filepath.Join(codexHome, "hooks.json")), "hook capture --agent codex") {
		t.Fatalf("after --fix: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(codexHome, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("--fix wrote an MCP entry next to the plugin's tools")
	}
}

func TestCodexPluginAndEntryAreDuplicates(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	codexHome := installCodexPlugin(t, home)
	config := filepath.Join(codexHome, "config.toml")
	entry := "[mcp_servers.github]\ncommand = \"gh-mcp\"\n\n[mcp_servers.kemory]\ncommand = \"kemory\"\nargs = [\"mcp\", \"serve\"]\n"
	writeFile(t, config, entry)

	_, report := runDoctor(t)
	if statusOf(report, "mcp host: codex") != "skip" || !strings.Contains(codexDetail(report), "duplicate tools") {
		t.Fatalf("a duplicate must be reported: %+v", report)
	}
	_, report = runDoctor(t, "--fix")
	if statusOf(report, "mcp host: codex") != "pass" {
		t.Fatalf("after --fix: %+v", report)
	}
	got := readFile(t, config)
	if strings.Contains(got, "mcp_servers.kemory") || !strings.Contains(got, "[mcp_servers.github]") {
		t.Fatalf("only the duplicate must go:\n%s", got)
	}
	if !strings.Contains(readFile(t, filepath.Join(codexHome, "hooks.json")), "hook session-start --agent codex") {
		t.Fatal("--fix did not add the hooks")
	}

	// connect removes a duplicate too.
	writeFile(t, config, entry)
	code, out, _ := run(t, "mcp", "install", "--host", "codex")
	if code != 0 || !strings.Contains(out, "listed every tool twice") {
		t.Fatalf("got %d %q", code, out)
	}
	if got := readFile(t, config); strings.Contains(got, "mcp_servers.kemory") || !strings.Contains(got, "[mcp_servers.github]") {
		t.Fatalf("connect must remove only the duplicate:\n%s", got)
	}
}
