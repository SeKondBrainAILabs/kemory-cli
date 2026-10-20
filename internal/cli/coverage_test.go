package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
)

func enablePlugin(t *testing.T, home string) {
	t.Helper()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"enabledPlugins": {"kemory@kemory": true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// localSignIn turns the saved sign-in into a local-mode one, which doctor
// does not try to refresh against an identity provider.
func localSignIn(t *testing.T) {
	t.Helper()
	creds, err := config.LoadCredentials("prod")
	if err != nil || creds == nil {
		t.Fatalf("no saved sign-in: %v", err)
	}
	creds.Issuer, creds.ClientID = "local", "local"
	if err := creds.Save(); err != nil {
		t.Fatal(err)
	}
}

func agentsReply(lastActive time.Time) func(http.ResponseWriter) {
	return jsonReply(200, fmt.Sprintf(`[
		{"status": "active", "agent_kind": "oauth", "client_slug": "chatgpt-web", "last_active_at": %q},
		{"status": "revoked", "agent_kind": "oauth", "client_slug": "claude-web", "last_active_at": %q},
		{"status": "active", "agent_kind": "oauth", "client_slug": "claude-web", "last_active_at": %q}]`,
		time.Now().Format(time.RFC3339Nano), time.Now().Format(time.RFC3339Nano), lastActive.Format(time.RFC3339Nano)))
}

func TestConnectDoesNotDuplicateThePlugin(t *testing.T) {
	home := setHome(t)
	enablePlugin(t, home)
	srv := fakeKemory(t)
	saveCreds(t, srv.URL)

	code, out, _ := run(t, "connect")
	if code != 0 || !strings.Contains(out, "already connected through the Kemory plugin") {
		t.Fatalf("got %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Fatal("wrote a second Kemory server next to the plugin's")
	}

	// The plugin connects to prod; another env still gets its entry.
	if code, _, _ := run(t, "--env", "india", "mcp", "install"); code != 0 || mcpServers(t, filepath.Join(home, ".claude.json"))["kemory-india"] == nil {
		t.Fatal("the india entry must still be written")
	}
}

func TestDoctorFindsAndRemovesAnEntryThatDuplicatesThePlugin(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	enablePlugin(t, home)
	claudeJSON := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(claudeJSON, []byte(`{"numStartups": 3, "mcpServers": {"kemory": {"command": "kemory"}, "github": {}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, report := runDoctor(t)
	if statusOf(report, "mcp host: claude-code") != "skip" {
		t.Fatalf("a duplicate must be reported: %+v", report)
	}
	_, report = runDoctor(t, "--fix")
	if statusOf(report, "mcp host: claude-code") != "pass" {
		t.Fatalf("after --fix: %+v", report)
	}
	servers := mcpServers(t, claudeJSON)
	if servers["kemory"] != nil || servers["github"] == nil {
		t.Fatalf("only the duplicate must go: %v", servers)
	}
}

func TestTheClaudeAIConnectorCoversClaudeDesktop(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	installApp(t, home, filepath.Join("Library", "Application Support", "Claude"))
	newFakeVault(t, map[string]func(http.ResponseWriter){"/api/v1/agents": agentsReply(time.Now().Add(-2 * time.Hour))})
	localSignIn(t)
	desktopConfig := hosts.ResolveConfig("claude-desktop")

	code, out, _ := run(t, "mcp", "install", "--host", "all")
	if code != 0 || !strings.Contains(out, "already connected through the claude.ai connector") {
		t.Fatalf("got %d %q", code, out)
	}
	if _, err := os.Stat(desktopConfig); !os.IsNotExist(err) {
		t.Fatal("wrote an entry the claude.ai connector already provides")
	}
	_, report := runDoctor(t)
	if statusOf(report, "mcp host: claude-desktop") != "pass" {
		t.Fatalf("doctor: %+v", report)
	}

	// An entry written before the connector existed is a duplicate; --fix
	// removes it while Claude Desktop is closed.
	if err := os.WriteFile(desktopConfig, []byte(`{"mcpServers": {"kemory": {"command": "kemory"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, report = runDoctor(t); statusOf(report, "mcp host: claude-desktop") != "skip" {
		t.Fatalf("duplicate not reported: %+v", report)
	}
	hosts.RunningCheck = func(string) bool { return true }
	if _, report = runDoctor(t, "--fix"); statusOf(report, "mcp host: claude-desktop") != "skip" {
		t.Fatalf("an open app must be left alone: %+v", report)
	}
	if _, err := os.Stat(desktopConfig); err != nil {
		t.Fatal("removed an entry from an open app's config")
	}
	hosts.RunningCheck = func(string) bool { return false }
	if _, report = runDoctor(t, "--fix"); statusOf(report, "mcp host: claude-desktop") != "pass" {
		t.Fatalf("after --fix: %+v", report)
	}
	if _, err := os.Stat(desktopConfig); !os.IsNotExist(err) {
		t.Fatal("the config kemory alone filled should be gone")
	}
}

func TestAnIdleConnectorDoesNotCount(t *testing.T) {
	home := setHome(t)
	installApp(t, home, filepath.Join("Library", "Application Support", "Claude"))
	newFakeVault(t, map[string]func(http.ResponseWriter){"/api/v1/agents": agentsReply(time.Now().Add(-45 * 24 * time.Hour))})
	if code, _, _ := run(t, "mcp", "install", "--host", "claude-desktop"); code != 0 || mcpServers(t, hosts.ResolveConfig("claude-desktop"))["kemory"] == nil {
		t.Fatal("a connector unused for 45 days must not stop the local entry")
	}
}

func loadClaudeCodePlugin(t *testing.T, home string) {
	t.Helper()
	enablePlugin(t, home)
	path := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version": 2, "plugins": {"kemory@kemory": [{"scope": "user"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func installCursorPlugin(t *testing.T, home string) {
	t.Helper()
	path := filepath.Join(home, ".cursor", "plugins", "local", "kemory", ".cursor-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"name": "kemory"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAKemoryPluginCoversCursor(t *testing.T) {
	for name, install := range map[string]func(*testing.T, string){
		"cursor plugin":      installCursorPlugin,
		"claude code plugin": loadClaudeCodePlugin,
	} {
		t.Run(name, func(t *testing.T) {
			home := setHome(t)
			install(t, home)
			srv := fakeKemory(t)
			saveCreds(t, srv.URL)

			code, out, _ := run(t, "connect", "--host", "cursor")
			if code != 0 || !strings.Contains(out, "already connected through") {
				t.Fatalf("got %d %q", code, out)
			}
			for _, f := range []string{"mcp.json", "hooks.json"} {
				if _, err := os.Stat(filepath.Join(home, ".cursor", f)); !os.IsNotExist(err) {
					t.Fatalf("wrote %s next to the plugin", f)
				}
			}

			// The plugins connect to prod; another env still gets its entry.
			if code, _, _ := run(t, "--env", "india", "mcp", "install", "--host", "cursor"); code != 0 ||
				mcpServers(t, filepath.Join(home, ".cursor", "mcp.json"))["kemory-india"] == nil {
				t.Fatal("the india entry must still be written")
			}
		})
	}
}

func TestDoctorRemovesWhatAnEarlierConnectLeftInCursor(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	mcpJSON := filepath.Join(home, ".cursor", "mcp.json")
	hooksJSON := filepath.Join(home, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(mcpJSON), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcpJSON, []byte(`{"mcpServers": {"kemory": {"command": "kemory"}, "github": {}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := hosts.InstallCursorHooks(hosts.LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	loadClaudeCodePlugin(t, home)
	srv := fakeKemory(t)
	saveCreds(t, srv.URL)

	if code, out, _ := run(t, "connect", "--host", "cursor"); code != 0 || !strings.Contains(out, "kemory doctor --fix") {
		t.Fatalf("connect must point at the leftovers: %d %q", code, out)
	}
	localSignIn(t)
	_, report := runDoctor(t)
	if statusOf(report, "mcp host: cursor") != "skip" {
		t.Fatalf("the duplicate must be reported: %+v", report)
	}
	_, report = runDoctor(t, "--fix")
	if statusOf(report, "mcp host: cursor") != "pass" {
		t.Fatalf("after --fix: %+v", report)
	}
	servers := mcpServers(t, mcpJSON)
	if servers["kemory"] != nil || servers["github"] == nil {
		t.Fatalf("only the duplicate must go: %v", servers)
	}
	if hosts.CursorHooksFound() {
		t.Fatalf("kemory hooks left in %s", hooksJSON)
	}

	// Hooks alone, without an entry, are a duplicate too.
	if _, err := hosts.InstallCursorHooks(hosts.LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if _, report = runDoctor(t); statusOf(report, "mcp host: cursor") != "skip" {
		t.Fatalf("leftover hooks must be reported: %+v", report)
	}
}
