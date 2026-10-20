package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

func setHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	for _, k := range []string{"KEMORY_ENV", "KEMORY_OUTPUT", "NO_COLOR", "KEMORY_API_KEY", "KEMORY_LAUNCHER", "UV", "XDG_CONFIG_HOME", "CODEX_HOME", "COPILOT_HOME"} {
		t.Setenv(k, "")
	}
	// Whether Claude Desktop is open on the machine running the tests must not
	// change what they see.
	running, inside, quit, open, handOff := hosts.RunningCheck, hosts.InsideApp, hosts.QuitApp, hosts.OpenApp, hosts.HandOff
	hosts.RunningCheck = func(string) bool { return false }
	hosts.InsideApp = func(string) bool { return false }
	hosts.QuitApp = func(string, time.Duration) error { t.Fatal("a test tried to quit a real app"); return nil }
	hosts.OpenApp = func(string) error { t.Fatal("a test tried to open a real app"); return nil }
	hosts.HandOff = func(string) error { t.Fatal("a test tried to open Terminal"); return nil }
	t.Cleanup(func() {
		hosts.RunningCheck, hosts.InsideApp, hosts.QuitApp, hosts.OpenApp, hosts.HandOff = running, inside, quit, open, handOff
	})
	// No test runs a real `claude` or `grok`: by default neither is installed.
	look, runner := lookPath, runTool
	// kemory is installed; the plugin tools are not.
	lookPath = func(name string) (string, error) {
		if name == "kemory" {
			return "/usr/local/bin/kemory", nil
		}
		return "", exec.ErrNotFound
	}
	runTool = func(name string, args ...string) ([]byte, error) {
		t.Fatalf("a test ran %s %v", name, args)
		return nil, nil
	}
	t.Cleanup(func() { lookPath, runTool = look, runner })
	// Nor sees the machine's own macOS apps.
	appDirs := hosts.ApplicationsDirs
	hosts.ApplicationsDirs = func() []string { return []string{filepath.Join(dir, "Applications")} }
	t.Cleanup(func() { hosts.ApplicationsDirs = appDirs })
	// No test runs a real package manager either.
	upgrader := runUpgrade
	runUpgrade = func(argv []string) error { t.Fatalf("a test ran %v", argv); return nil }
	t.Cleanup(func() { runUpgrade = upgrader })
	// The guided screen needs a real terminal; tests get plain output.
	width := terminalWidth
	terminalWidth = func() int { return 0 }
	t.Cleanup(func() { terminalWidth = width })
	// MCP host configs on Windows live under %APPDATA%.
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))
	// Tests never reach a real Kemory: anything not pointed at a fake server
	// goes to a closed local port.
	t.Setenv("KEMORY_URL", "http://127.0.0.1:1")
	t.Setenv("KEMORY_OIDC_ISSUER", "http://127.0.0.1:1")
	return dir
}

// run executes the CLI and returns exit code, stdout and stderr.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	render.Out, render.Err = &out, &errOut
	defer func() { render.Out, render.Err = os.Stdout, os.Stderr }()
	code := Execute(args)
	return code, out.String(), errOut.String()
}

const meBody = `{"user_id": "u-1", "email": "ada@example.com", "org_id": "org-1", "org_name": "org-1",
	"org_role": "owner", "roles": ["admin"], "teams": [{"name": "core", "role": "member", "can_write": true}]}`

func fakeAPI(t *testing.T, orgs string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/me":
			w.Write([]byte(meBody))
		case "/api/v1/me/orgs":
			w.Write([]byte(orgs))
		case "/api/v1/residency/cell":
			w.Write([]byte(`{"is_home_cell": true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func saveCreds(t *testing.T, kemoryURL string) {
	t.Helper()
	c := &config.Credentials{
		AccessToken: "at", RefreshToken: "rt", ExpiresAt: float64(time.Now().Add(time.Hour).Unix()),
		Issuer: "https://i", ClientID: "kemory-cli", KemoryURL: kemoryURL, Env: "prod",
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestVersion(t *testing.T) {
	setHome(t)
	code, out, _ := run(t, "--version")
	if code != 0 || out != "kemory, version dev\n" {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestUnknownEnvFails(t *testing.T) {
	setHome(t)
	code, _, errOut := run(t, "--env", "staging", "whoami")
	if code != exits.Failure || !strings.Contains(errOut, "Error: unknown env 'staging'") {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestUnknownFlagIsAUsageError(t *testing.T) {
	setHome(t)
	if code, _, _ := run(t, "whoami", "--bogus"); code != exits.Usage {
		t.Fatalf("got %d", code)
	}
	if code, _, _ := run(t, "nope"); code != exits.Usage {
		t.Fatalf("unknown command: got %d", code)
	}
}

func TestWhoamiWithoutCredentialsIsAnAuthFailure(t *testing.T) {
	setHome(t)
	code, _, errOut := run(t, "whoami")
	if code != exits.Auth || !strings.Contains(errOut, "kemory login") {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestWhoamiText(t *testing.T) {
	setHome(t)
	srv := fakeAPI(t, `[{"org_id": "org-1", "name": "Acme"}]`)
	saveCreds(t, srv.URL)
	code, out, _ := run(t, "whoami")
	for _, want := range []string{"ada@example.com", "Acme (org-1)  role=owner", "admin", "core", "yes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestWhoamiJSONKeepsOnlyThePayloadOnStdout(t *testing.T) {
	setHome(t)
	srv := fakeAPI(t, `[]`)
	saveCreds(t, srv.URL)
	code, out, _ := run(t, "--output", "json", "whoami")
	var m map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &m) != nil || m["email"] != "ada@example.com" {
		t.Fatalf("got %d %q", code, out)
	}
	if !strings.HasPrefix(out, "{\n  \"user_id\"") {
		t.Fatalf("server key order must survive: %q", out[:20])
	}
}

func TestLogout(t *testing.T) {
	home := setHome(t)
	saveCreds(t, "https://k")
	code, out, _ := run(t, "logout")
	if code != 0 || !strings.Contains(out, "Removed") {
		t.Fatalf("got %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".kemory", "credentials-prod")); !os.IsNotExist(err) {
		t.Fatal("credentials still present")
	}
	if _, out, _ := run(t, "logout"); !strings.Contains(out, "no credentials file") {
		t.Fatalf("second logout: %q", out)
	}
}

func TestLoginLocal(t *testing.T) {
	setHome(t)
	t.Setenv("KEMORY_API_KEY", "key-123")
	code, out, _ := run(t, "login", "--local")
	c, _ := config.LoadCredentials("prod")
	if code != 0 || c == nil || c.AccessToken != "key-123" || !c.IsLocal() || !strings.Contains(out, "Local credential stored") {
		t.Fatalf("got %d %q %+v", code, out, c)
	}
}

func TestLoginDeviceFlowEndToEnd(t *testing.T) {
	setHome(t)
	api := fakeAPI(t, `[{"org_id": "org-1", "name": "Acme"}]`)
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/device") {
			w.Write([]byte(`{"device_code": "dc", "user_code": "WXYZ", "verification_uri": "https://idp/device"}`))
			return
		}
		w.Write([]byte(`{"access_token": "at", "refresh_token": "rt", "expires_in": 300}`))
	}))
	defer issuer.Close()
	t.Setenv("KEMORY_OIDC_ISSUER", issuer.URL)
	t.Setenv("KEMORY_URL", api.URL)
	auth.OpenBrowser = func(string) error { return nil }

	code, out, errOut := run(t, "login", "--no-browser")
	if code != 0 || !strings.Contains(out, "Code: WXYZ") || !strings.Contains(out, "Logged in as ada@example.com · org=org-1") {
		t.Fatalf("got %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	c, _ := config.LoadCredentials("prod")
	if c.Email != "ada@example.com" || c.OrgID != "org-1" || c.ActiveOrgID != "org-1" || c.KemoryURL != api.URL {
		t.Fatalf("saved: %+v", c)
	}
}

func TestLoginWithManyOrgsLeavesTheChoiceToTheUser(t *testing.T) {
	setHome(t)
	api := fakeAPI(t, `[{"org_id": "org-1"}, {"org_id": "org-2"}]`)
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/device") {
			w.Write([]byte(`{"device_code": "dc", "user_code": "WXYZ", "verification_uri": "https://idp/device"}`))
			return
		}
		io.WriteString(w, `{"access_token": "at", "refresh_token": "rt"}`)
	}))
	defer issuer.Close()
	t.Setenv("KEMORY_OIDC_ISSUER", issuer.URL)
	t.Setenv("KEMORY_URL", api.URL)
	_, out, _ := run(t, "login", "--no-browser")
	c, _ := config.LoadCredentials("prod")
	if c.ActiveOrgID != "" || !strings.Contains(out, "You belong to 2 orgs") {
		t.Fatalf("got %q, active=%q", out, c.ActiveOrgID)
	}
}
