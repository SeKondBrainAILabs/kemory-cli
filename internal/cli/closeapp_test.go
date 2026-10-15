package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
)

// fakeDesktop is an open Claude Desktop whose quits and opens are recorded.
type fakeDesktop struct {
	running bool
	events  []string
}

func openDesktop(t *testing.T, config string) *fakeDesktop {
	t.Helper()
	d := &fakeDesktop{running: true}
	hosts.RunningCheck = func(name string) bool { return name == "claude-desktop" && d.running }
	hosts.QuitApp = func(string, time.Duration) error {
		d.running = false
		d.events = append(d.events, "quit")
		return nil
	}
	hosts.OpenApp = func(string) error {
		_, err := os.Stat(config)
		d.events = append(d.events, map[bool]string{true: "open, config gone", false: "open, config present"}[os.IsNotExist(err)])
		d.running = true
		return nil
	}
	return d
}

// duplicateDesktopSetup is an installed Claude Desktop with a kemory entry
// that duplicates an active claude.ai connector.
func duplicateDesktopSetup(t *testing.T) string {
	t.Helper()
	home := setHome(t)
	fakeReleases(t, `[]`)
	installApp(t, home, filepath.Join("Library", "Application Support", "Claude"))
	newFakeVault(t, map[string]func(http.ResponseWriter){"/api/v1/agents": agentsReply(time.Now().Add(-time.Hour))})
	localSignIn(t)
	config := hosts.ResolveConfig("claude-desktop")
	if err := os.WriteFile(config, []byte(`{"mcpServers": {"kemory": {"command": "kemory"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestFixQuitsClaudeDesktopChangesTheConfigThenReopensIt(t *testing.T) {
	config := duplicateDesktopSetup(t)
	desktop := openDesktop(t, config)

	_, report := runDoctor(t, "--fix", "--yes")
	if statusOf(report, "mcp host: claude-desktop") != "pass" {
		t.Fatalf("doctor: %+v", report)
	}
	if strings.Join(desktop.events, "; ") != "quit; open, config gone" {
		t.Fatalf("the config must change between quitting and reopening: %v", desktop.events)
	}
}

func TestFixAsksBeforeQuittingClaudeDesktop(t *testing.T) {
	config := duplicateDesktopSetup(t)
	desktop := openDesktop(t, config)
	interactive := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = interactive })

	withStdin(t, "\n")
	stdinIsTerminal = func() bool { return true }
	if _, report := runDoctor(t, "--fix"); statusOf(report, "mcp host: claude-desktop") != "skip" || len(desktop.events) != 0 {
		t.Fatalf("declining must leave the app alone: %v %+v", desktop.events, report)
	}

	withStdin(t, "y\n")
	stdinIsTerminal = func() bool { return true }
	if _, report := runDoctor(t, "--fix"); statusOf(report, "mcp host: claude-desktop") != "pass" || len(desktop.events) != 2 {
		t.Fatalf("yes must quit, fix and reopen: %v %+v", desktop.events, report)
	}
}

func TestNeverQuitTheAppKemoryRunsInside(t *testing.T) {
	config := duplicateDesktopSetup(t)
	desktop := openDesktop(t, config)
	hosts.InsideApp = func(string) bool { return true }

	_, report := runDoctor(t, "--fix", "--yes")
	if len(desktop.events) != 0 {
		t.Fatalf("quit the app kemory runs inside: %v", desktop.events)
	}
	for _, c := range report.Checks {
		if c.Check == "mcp host: claude-desktop" && (c.Detail == nil || !strings.Contains(*c.Detail, "Terminal or iTerm")) {
			t.Fatalf("doctor: %+v", c)
		}
	}
	if _, err := os.Stat(config); err != nil {
		t.Fatal("the entry must stay until the fix can run outside the app")
	}
}
