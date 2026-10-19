package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pickerMachine has Cursor, Warp and Codex installed, Warp connected, Codex
// with an MCP entry but no memory hooks, a signed-in prod user and an
// interactive terminal answering answer.
func pickerMachine(t *testing.T, answer string) string {
	t.Helper()
	home := setHome(t)
	srv := fakeKemory(t)
	saveCreds(t, srv.URL)
	installApp(t, home, ".cursor")
	installApp(t, home, ".warp")
	writeFile(t, filepath.Join(home, ".warp", ".mcp.json"), `{"mcpServers": {"kemory": {"command": "kemory"}}}`)
	installApp(t, home, ".codex")
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), "[mcp_servers.kemory]\ncommand = \"kemory\"\nargs = [\"mcp\", \"serve\"]\n")
	interactive := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminal = interactive })
	withStdin(t, answer)
	stdinIsTerminal = func() bool { return true }
	return home
}

func TestConnectListsClientsAndConnectsTheReadyOnesOnEnter(t *testing.T) {
	home := pickerMachine(t, "\n")
	code, out, errOut := run(t, "connect")
	if code != 0 {
		t.Fatalf("got %d\n%s\n%s", code, out, errOut)
	}
	for _, want := range []string{
		"[ready    ] cursor", "add the MCP entry to", "[connected] warp",
		"[ready    ] codex", "add automatic memory hooks (MCP entry already in", "not found:",
	} {
		if !strings.Contains(out+errOut, want) {
			t.Fatalf("missing %q in:\n%s\n%s", want, out, errOut)
		}
	}
	if mcpServers(t, filepath.Join(home, ".cursor", "mcp.json"))["kemory"] == nil {
		t.Fatal("Enter must connect every ready client")
	}
	if !strings.Contains(readFile(t, filepath.Join(home, ".codex", "hooks.json")), "hook session-start --agent codex") {
		t.Fatal("Codex must get its memory hooks")
	}
	if !strings.Contains(out, "Connected as") {
		t.Fatalf("the connection check must still run:\n%s", out)
	}
}

func TestConnectPicksByNumberAndNoneChangesNothing(t *testing.T) {
	home := pickerMachine(t, "none\n")
	if code, _, _ := run(t, "connect"); code != 0 {
		t.Fatal(code)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "mcp.json")); !os.IsNotExist(err) {
		t.Fatal("none must change nothing")
	}

	withStdin(t, "9\n")
	stdinIsTerminal = func() bool { return true }
	if code, _, errOut := run(t, "connect"); code != 2 || !strings.Contains(errOut, "not one of the numbers listed") {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestDryRunShowsThePlanAndChangesNothing(t *testing.T) {
	home := pickerMachine(t, "\n")
	code, out, _ := run(t, "connect", "--dry-run")
	if code != 0 || !strings.Contains(out, "→ cursor") || !strings.Contains(out, "would add the MCP entry") || !strings.Contains(out, "Dry run: nothing was changed") {
		t.Fatalf("got %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "mcp.json")); !os.IsNotExist(err) {
		t.Fatal("a dry run wrote a config")
	}
	if code, out, _ := run(t, "mcp", "install", "--host", "cursor", "--dry-run"); code != 0 || !strings.Contains(out, "would add the MCP entry") {
		t.Fatalf("mcp install --dry-run: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "mcp.json")); !os.IsNotExist(err) {
		t.Fatal("mcp install --dry-run wrote a config")
	}
}

func TestScriptsGetNoPrompt(t *testing.T) {
	for _, args := range [][]string{{"connect", "--host", "cursor"}, {"--output", "json", "connect"}} {
		home := pickerMachine(t, "none\n")
		code, out, errOut := run(t, args...)
		if code != 0 || strings.Contains(out+errOut, "Connect which?") {
			t.Fatalf("%v prompted: %d\n%s\n%s", args, code, out, errOut)
		}
		if args[0] == "connect" && mcpServers(t, filepath.Join(home, ".cursor", "mcp.json"))["kemory"] == nil {
			t.Fatalf("%v: --host must connect without asking", args)
		}
	}
}
