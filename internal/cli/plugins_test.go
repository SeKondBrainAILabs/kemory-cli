package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTools makes `claude` and `grok` available and records their calls.
func fakeTools(t *testing.T, reply func(name string, args []string) ([]byte, error)) *[]string {
	t.Helper()
	var calls []string
	lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	runTool = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return reply(name, args)
	}
	return &calls
}

func TestConnectInstallsTheClaudeCodePluginInsteadOfAnEntry(t *testing.T) {
	home := setHome(t)
	calls := fakeTools(t, func(_ string, args []string) ([]byte, error) {
		if strings.Join(args, " ") == "plugin marketplace list --json" {
			return []byte(`[{"name": "claude-plugins-official", "repo": "anthropics/claude-plugins-official"}]`), nil
		}
		return []byte("ok"), nil
	})

	code, out, _ := run(t, "mcp", "install", "--host", "claude-code")
	if code != 0 || !strings.Contains(out, "installed the Kemory plugin") {
		t.Fatalf("got %d %q", code, out)
	}
	want := "claude plugin marketplace list --json; claude plugin marketplace add SeKondBrainAILabs/kemory-plugin; claude plugin install kemory@kemory --scope user"
	if got := strings.Join(*calls, "; "); got != want {
		t.Fatalf("calls:\n%s\nwant:\n%s", got, want)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Fatal("wrote an MCP entry next to the plugin")
	}
}

func TestAKnownMarketplaceIsNotAddedAgain(t *testing.T) {
	setHome(t)
	calls := fakeTools(t, func(_ string, args []string) ([]byte, error) {
		if strings.Join(args, " ") == "plugin marketplace list --json" {
			return []byte(`[{"name": "kemory-team", "repo": "sekondbrainailabs/kemory-plugin"}]`), nil
		}
		return nil, nil
	})
	run(t, "mcp", "install", "--host", "claude-code")
	if got := strings.Join(*calls, "; "); got != "claude plugin marketplace list --json; claude plugin install kemory@kemory-team --scope user" {
		t.Fatalf("calls: %s", got)
	}
}

func TestAFailedPluginInstallFallsBackToTheEntry(t *testing.T) {
	home := setHome(t)
	fakeTools(t, func(_ string, args []string) ([]byte, error) {
		if args[0] == "plugin" && args[1] == "install" {
			return []byte("Cloning…\nerror: network unreachable"), errors.New("exit status 1")
		}
		return []byte(`[]`), nil
	})
	code, out, _ := run(t, "mcp", "install", "--host", "grok")
	if code != 0 || !strings.Contains(out, "could not be installed") || !strings.Contains(out, "network unreachable") {
		t.Fatalf("got %d %q", code, out)
	}
	if !strings.Contains(readFile(t, filepath.Join(home, ".grok", "config.toml")), "[mcp_servers.kemory]") {
		t.Fatal("the fallback MCP entry was not written")
	}
}

func TestWithoutTheHostCLIConnectWritesTheEntryAndSaysWhy(t *testing.T) {
	home := setHome(t)
	code, out, _ := run(t, "mcp", "install", "--host", "claude-code")
	if code != 0 || !strings.Contains(out, "the `claude` command was not found") {
		t.Fatalf("got %d %q", code, out)
	}
	if mcpServers(t, filepath.Join(home, ".claude.json"))["kemory"] == nil {
		t.Fatal("the fallback MCP entry was not written")
	}
}

func TestOtherEnvironmentsStillGetAnEntry(t *testing.T) {
	home := setHome(t)
	calls := fakeTools(t, func(string, []string) ([]byte, error) { return nil, nil })
	if code, _, _ := run(t, "--env", "india", "mcp", "install", "--host", "claude-code"); code != 0 {
		t.Fatal(code)
	}
	if len(*calls) != 0 || mcpServers(t, filepath.Join(home, ".claude.json"))["kemory-india"] == nil {
		t.Fatalf("the plugin connects to prod; india needs its entry: %v", *calls)
	}
}

func TestDoctorTreatsTheGrokPluginAsConnectedAndRemovesTheDuplicate(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	grok := filepath.Join(home, ".grok")
	writeFile(t, filepath.Join(grok, "installed-plugins", "registry.json"),
		`{"version": 1, "repos": {"kemory-plugin-x": {"plugins": {"kemory": {"version": "0.16.0"}}}}}`)
	config := filepath.Join(grok, "config.toml")
	writeFile(t, config, "[plugins]\nenabled = [\"kemory\"]\n\n[mcp_servers.kemory]\ncommand = \"kemory\"\nargs = [\"mcp\", \"serve\"]\n")

	if _, report := runDoctor(t); statusOf(report, "mcp host: grok") != "skip" {
		t.Fatalf("the duplicate must be reported: %+v", report)
	}
	if _, report := runDoctor(t, "--fix"); statusOf(report, "mcp host: grok") != "pass" {
		t.Fatalf("after --fix: %+v", report)
	}
	got := readFile(t, config)
	if strings.Contains(got, "mcp_servers") || !strings.Contains(got, `enabled = ["kemory"]`) {
		t.Fatalf("only the duplicate entry must go:\n%s", got)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
