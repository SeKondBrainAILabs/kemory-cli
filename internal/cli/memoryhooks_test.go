package cli

import (
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
