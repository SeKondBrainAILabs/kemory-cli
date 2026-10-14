package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/releases"
)

const (
	orgAcme  = "11111111-1111-4111-8111-111111111111"
	orgOther = "22222222-2222-4222-8222-222222222222"
)

// fakeKemory serves /health/live, /api/v1/me (honouring X-Organization-ID)
// and /api/v1/me/orgs. Requests for org "deny" are refused.
func fakeKemory(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/me":
			if r.Header.Get("X-Organization-ID") == orgOther {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"email": "ada@example.com", "org_id": "` + orgAcme + `", "org_name": "Acme", "org_role": "owner", "teams": []}`))
		case "/api/v1/me/orgs":
			_, _ = w.Write([]byte(`[{"org_id": "` + orgOther + `", "name": "Other Co", "org_type": "organisation", "role": "member"},
				{"org_id": "` + orgAcme + `", "name": "Acme", "org_type": "personal", "role": "owner", "active": true}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func installApp(t *testing.T, home, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, dir, "app-state"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mcpServers(t *testing.T, path string) map[string]map[string]any {
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

func TestMCPInstallDefaultsToClaudeCode(t *testing.T) {
	home := setHome(t)
	code, out, _ := run(t, "mcp", "install")
	if code != 0 || !strings.Contains(out, "claude-code") || !strings.Contains(out, "Restart") {
		t.Fatalf("got %d %q", code, out)
	}
	if mcpServers(t, filepath.Join(home, ".claude.json"))["kemory"]["command"] != "kemory" {
		t.Fatal("entry not written")
	}
}

func TestMCPInstallAllWiresOnlyInstalledHosts(t *testing.T) {
	home := setHome(t)
	if code, _, errOut := run(t, "mcp", "install", "--host", "all"); code != exits.Failure || !strings.Contains(errOut, "No supported MCP host") {
		t.Fatalf("no hosts: got %d %q", code, errOut)
	}
	installApp(t, home, ".cursor")
	code, out, errOut := run(t, "--env", "india", "mcp", "install", "--host", "all")
	if code != 0 || !strings.Contains(out, "cursor") || !strings.Contains(errOut, "warp") {
		t.Fatalf("got %d %q %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(home, ".warp")); !os.IsNotExist(err) {
		t.Fatal("a config dir was created for an app that is not installed")
	}
	if mcpServers(t, filepath.Join(home, ".cursor", "mcp.json"))["kemory-india"] == nil {
		t.Fatal("the india env must get its own server name")
	}
}

func TestMCPInstallRejectsAnUnknownHost(t *testing.T) {
	setHome(t)
	if code, _, _ := run(t, "mcp", "install", "--host", "vim"); code != exits.Usage {
		t.Fatalf("got %d", code)
	}
}

func TestConnectWhenSignedInWiresAndChecks(t *testing.T) {
	home := setHome(t)
	srv := fakeKemory(t)
	saveCreds(t, srv.URL)
	code, out, _ := run(t, "connect", "--host", "cursor")
	if code != 0 || !strings.Contains(out, "Already logged in") || !strings.Contains(out, "Connected as ada@example.com") {
		t.Fatalf("got %d %q", code, out)
	}
	if mcpServers(t, filepath.Join(home, ".cursor", "mcp.json"))["kemory"] == nil {
		t.Fatal("entry not written")
	}
}

func TestConnectSignsInFirstWhenNeeded(t *testing.T) {
	setHome(t)
	t.Setenv("KEMORY_API_KEY", "key-1")
	code, out, errOut := run(t, "connect", "--local")
	if code != 0 || !strings.Contains(out, "starting login") || !strings.Contains(out, "Local credential stored") {
		t.Fatalf("got %d %q %q", code, out, errOut)
	}
	// The check call fails (no local Kemory) but connect still succeeds.
	if !strings.Contains(errOut, "Host config written") {
		t.Fatalf("a failed check must be a warning: %q", errOut)
	}
}

func TestOrgs(t *testing.T) {
	setHome(t)
	saveCreds(t, fakeKemory(t).URL)
	code, out, _ := run(t, "orgs")
	if code != 0 || strings.Index(out, "Acme") > strings.Index(out, "Other Co") || !strings.Contains(out, "●") {
		t.Fatalf("the active org comes first: %d\n%s", code, out)
	}
	_, out, _ = run(t, "--output", "json", "orgs")
	var orgs []map[string]any
	if json.Unmarshal([]byte(out), &orgs) != nil || len(orgs) != 2 {
		t.Fatalf("json: %q", out)
	}
}

func TestUseByNameAndPrefixAndClear(t *testing.T) {
	setHome(t)
	saveCreds(t, fakeKemory(t).URL)
	if code, out, _ := run(t, "use", "acme"); code != 0 || !strings.Contains(out, "Active org → Acme") {
		t.Fatalf("by name: %d %q", code, out)
	}
	if c, _ := config.LoadCredentials("prod"); c.ActiveOrgID != orgAcme {
		t.Fatalf("not saved: %q", c.ActiveOrgID)
	}
	if code, _, _ := run(t, "use", "1111"); code != 0 {
		t.Fatalf("by prefix: %d", code)
	}
	if code, out, _ := run(t, "use", "--clear"); code != 0 || !strings.Contains(out, "Cleared") {
		t.Fatalf("clear: %d %q", code, out)
	}
	if c, _ := config.LoadCredentials("prod"); c.ActiveOrgID != "" {
		t.Fatal("selection not cleared")
	}
}

func TestUseFailuresLeaveTheSelectionAlone(t *testing.T) {
	setHome(t)
	saveCreds(t, fakeKemory(t).URL)
	run(t, "use", orgAcme)
	cases := map[string][]string{
		"no match":  {"use", "Globex"},
		"refused":   {"use", orgOther},
		"both":      {"use", "acme", "--clear"},
		"no target": {"use"},
	}
	for name, args := range cases {
		code, _, errOut := run(t, args...)
		if code == 0 {
			t.Fatalf("%s: succeeded: %q", name, errOut)
		}
		if c, _ := config.LoadCredentials("prod"); c.ActiveOrgID != orgAcme {
			t.Fatalf("%s: selection changed to %q", name, c.ActiveOrgID)
		}
	}
}

func fakeReleases(t *testing.T, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	old := releases.URL
	releases.URL = srv.URL
	t.Cleanup(func() { releases.URL = old })
}

func fakeIssuer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token": "at", "refresh_token": "rt2", "expires_in": 300}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

type doctorReport struct {
	OK     bool `json:"ok"`
	Checks []struct {
		Check  string  `json:"check"`
		Status string  `json:"status"`
		Detail *string `json:"detail"`
	} `json:"checks"`
}

func runDoctor(t *testing.T, args ...string) (int, doctorReport) {
	t.Helper()
	code, out, errOut := run(t, append([]string{"--output", "json", "doctor"}, args...)...)
	var report doctorReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("doctor json: %v\nstdout: %s\nstderr: %s", err, out, errOut)
	}
	return code, report
}

func statusOf(r doctorReport, check string) string {
	for _, c := range r.Checks {
		if c.Check == check {
			return c.Status
		}
	}
	return "missing"
}

func TestDoctorHealthySetup(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[{"tag_name": "cli-v0.6.14", "assets": [{"name": "kemory-linux-x64.tar.gz"}]}]`)
	api := fakeKemory(t)
	issuer := fakeIssuer(t)
	c := &config.Credentials{AccessToken: "at", RefreshToken: "rt", ExpiresAt: 1, Issuer: issuer.URL, ClientID: "kemory-cli", KemoryURL: api.URL, Env: "prod"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	installApp(t, home, ".claude")
	run(t, "mcp", "install")

	code, report := runDoctor(t)
	for _, check := range []string{"credentials cached", "kemory reachable", "token refresh", "auth round-trip", "mcp host: claude-code"} {
		if statusOf(report, check) != "pass" {
			t.Fatalf("%s: %s\n%+v", check, statusOf(report, check), report)
		}
	}
	if statusOf(report, "cli up-to-date") != "skip" { // a dev build cannot be compared
		t.Fatalf("freshness: %s", statusOf(report, "cli up-to-date"))
	}
	if code != 0 || !report.OK {
		t.Fatalf("code %d ok %v", code, report.OK)
	}
}

func TestDoctorReportsAndFixesHosts(t *testing.T) {
	home := setHome(t)
	fakeReleases(t, `[]`)
	installApp(t, home, ".cursor")
	if err := os.MkdirAll(filepath.Join(home, ".warp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".warp", ".mcp.json"), []byte(`{"mcpServers": {"kemory": {}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	code, report := runDoctor(t)
	if code != 0 || report.OK || statusOf(report, "credentials cached") != "fail" {
		t.Fatalf("doctor must exit 0 and report ok=false: %d %+v", code, report)
	}
	if statusOf(report, "mcp host: cursor") != "skip" || statusOf(report, "mcp host: warp") != "skip" {
		t.Fatalf("hosts: %+v", report)
	}

	_, report = runDoctor(t, "--fix")
	if statusOf(report, "mcp host: cursor") != "pass" || statusOf(report, "mcp host: warp") != "pass" {
		t.Fatalf("after --fix: %+v", report)
	}
	if mcpServers(t, filepath.Join(home, ".cursor", "mcp.json"))["kemory"] == nil {
		t.Fatal("cursor not wired")
	}
	if _, err := os.Stat(filepath.Join(home, ".warp")); !os.IsNotExist(err) {
		t.Fatal("the stale warp entry kemory created should be removed")
	}
}

func TestDoctorTextOutput(t *testing.T) {
	setHome(t)
	fakeReleases(t, `[]`)
	code, out, _ := run(t, "doctor")
	if code != 0 || !strings.Contains(out, "FAIL  credentials cached") || !strings.Contains(out, "kemory doctor --fix") {
		t.Fatalf("got %d\n%s", code, out)
	}
}
