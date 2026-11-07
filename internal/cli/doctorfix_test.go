package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
)

// detailOf is the detail doctor printed for a check.
func detailOf(r doctorReport, check string) string {
	for _, c := range r.Checks {
		if c.Check == check && c.Detail != nil {
			return *c.Detail
		}
	}
	return ""
}

func installWarp(t *testing.T, home string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, ".warp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".warp", "settings.yaml"), []byte("x: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorFixNamesTheAppToRestart(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	// VS Code's user dir depends on the OS, so take it from its config path.
	userDir, err := filepath.Rel(home, filepath.Dir(hosts.ConfigPaths("vscode")[0]))
	if err != nil {
		t.Fatal(err)
	}
	installApp(t, home, userDir)

	_, report := runDoctor(t, "--fix")
	detail := detailOf(report, "mcp host: vscode")
	if statusOf(report, "mcp host: vscode") != "pass" || !strings.Contains(detail, "restart VS Code") || strings.Contains(detail, "restart vscode") {
		t.Fatalf("vscode: %q\n%+v", detail, report)
	}
}

func TestDoctorFixDoesNotTellWarpToRestart(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	installWarp(t, home)

	_, report := runDoctor(t, "--fix")
	detail := detailOf(report, "mcp host: warp")
	// The temp dir is named after the test, so keep its path out of the match.
	if statusOf(report, "mcp host: warp") != "pass" || strings.Contains(strings.ToLower(strings.ReplaceAll(detail, home, "")), "restart") || strings.HasSuffix(detail, " — ") {
		t.Fatalf("warp: %q\n%+v", detail, report)
	}
}

func TestDoctorFixRewriteDoesNotTellWarpToRestart(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	installWarp(t, home)
	gone := filepath.Join(home, "gone", "kemory")
	// Marshal the path: a Windows path has backslashes that are not valid raw JSON.
	command, err := json.Marshal(gone)
	if err != nil {
		t.Fatal(err)
	}
	entry := `{"mcpServers": {"kemory": {"command": ` + string(command) + `}}}`
	if err := os.WriteFile(filepath.Join(home, ".warp", ".mcp.json"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}

	_, report := runDoctor(t, "--fix")
	detail := detailOf(report, "mcp host: warp")
	if statusOf(report, "mcp host: warp") != "pass" || !strings.HasPrefix(detail, "rewrote the kemory entry in ") || strings.Contains(strings.ToLower(strings.ReplaceAll(detail, home, "")), "restart") {
		t.Fatalf("warp rewrite: %q\n%+v", detail, report)
	}
}

func TestDoctorFixKeepsTheRestartForCodex(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	installApp(t, home, ".codex")

	_, report := runDoctor(t, "--fix")
	detail := detailOf(report, "mcp host: codex")
	if statusOf(report, "mcp host: codex") != "pass" || !strings.Contains(detail, "restart Codex") || !strings.Contains(detail, "approve them once with /hooks in Codex") {
		t.Fatalf("codex: %q\n%+v", detail, report)
	}
}

func TestDoctorFixShowsTheReopenNoteInsteadOfRestart(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	installApp(t, home, filepath.Join("Library", "Application Support", "Claude"))
	openDesktop(t, filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"))

	_, report := runDoctor(t, "--fix", "--yes")
	detail := detailOf(report, "mcp host: claude-desktop")
	if statusOf(report, "mcp host: claude-desktop") != "pass" || !strings.Contains(detail, "Claude Desktop was restarted") || strings.Contains(detail, "restart Claude Desktop") {
		t.Fatalf("desktop: %q\n%+v", detail, report)
	}
}
