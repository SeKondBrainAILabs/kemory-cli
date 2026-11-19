package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/residency"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/version"
)

var sgrCodes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// plainOutput sends render's output to a buffer in plain, uncoloured mode and
// restores the package state when the test ends.
func plainOutput(t *testing.T) *strings.Builder {
	t.Helper()
	out := &strings.Builder{}
	oldOut, oldWidth := render.Out, render.CheckLabelWidth
	render.Out = out
	render.Configure(true, false, false)
	t.Cleanup(func() {
		render.Out, render.CheckLabelWidth = oldOut, oldWidth
		render.Configure(false, false, false)
	})
	return out
}

// reportOf is what d.report() prints, with colour codes removed.
func reportOf(t *testing.T, d *doctor) string {
	t.Helper()
	setHome(t)
	out := plainOutput(t)
	d.report()
	return sgrCodes.ReplaceAllString(out.String(), "")
}

// row is one plain check line: the tag, the label padded to render's fixed
// 26 columns, then the detail.
func row(tag, label, detail string) string {
	return fmt.Sprintf("%s  %-26s  %s\n", tag, label, detail)
}

func TestDoctorReportShowsAPassingSetupInPlainWords(t *testing.T) {
	srv := fakeAPI(t, `[{"org_id": "org-1", "name": "Acme"}]`)
	d := &doctor{
		fresh: &config.Credentials{AccessToken: "at", KemoryURL: srv.URL},
		checks: []checkResult{
			passCheck("credentials cached", "H/.kemory/credentials.json, access token expires 19:24 (in 59m)"),
			passCheck("kemory reachable", "GET /health/live → 200"),
			passCheck("token refresh", "new access token expires 19:24 (in 59m)"),
			passCheck("auth round-trip", "org=org-1, teams=1"),
			passCheck("mcp host: cursor", "/x/.cursor/mcp.json"),
			passCheck("mcp host: claude-code", "/x/.claude.json — automatic memory hooks installed"),
			passCheck("cli up-to-date", "latest is cli-v0.6.14"),
		},
	}

	got := reportOf(t, d)

	want := "\nAccount\n" +
		row("PASS", "Signed in", "access token expires 19:24 (in 59m)") +
		row("PASS", "Kemory reachable", "yes") +
		row("PASS", "Session refresh", "works") +
		row("PASS", "Organisation", "Acme") +
		"\nAI clients\n" +
		row("PASS", "Cursor", "on demand") +
		row("PASS", "Claude Code (Code tab, terminal)", "automatic") +
		"\nCLI\n" +
		row("PASS", "Up to date", version.Version)
	if got != want {
		t.Fatalf("report:\n%q\nwant:\n%q", got, want)
	}
	if render.CheckLabelWidth != len("Claude Code (Code tab, terminal)") {
		t.Fatalf("CheckLabelWidth = %d, want the longest label (32)", render.CheckLabelWidth)
	}
}

func TestDoctorReportKeepsTheFullHintForEveryProblem(t *testing.T) {
	d := &doctor{checks: []checkResult{
		problemCheck("credentials cached", render.Fail, "run `kemory login` (or `kemory login --local`)"),
		problemCheck("kemory reachable", render.Fail, "dial tcp 127.0.0.1:1: connect: connection refused"),
		problemCheck("token refresh", render.Skip, "no credentials"),
		problemCheck("auth round-trip", render.Skip, "no usable credentials"),
		problemCheck("mcp host: vscode", render.Skip, "installed, not connected"),
		problemCheck("mcp host config", render.Fail, "no installed MCP host has a kemory entry"),
		problemCheck("cli up-to-date", render.Fail, "version cli-v9.0.0 is available — run `kemory upgrade`"),
	}}

	got := reportOf(t, d)

	want := "\nAccount\n" +
		row("FAIL", "Signed in", "run `kemory login` (or `kemory login --local`)") +
		row("FAIL", "Kemory reachable", "dial tcp 127.0.0.1:1: connect: connection refused") +
		row("SKIP", "Session refresh", "no credentials") +
		row("SKIP", "Organisation", "no usable credentials") +
		"\nAI clients\n" +
		row("SKIP", "VS Code", "installed, not connected") +
		row("FAIL", "Any client", "no installed MCP host has a kemory entry") +
		"\nCLI\n" +
		row("FAIL", "Up to date", "version cli-v9.0.0 is available — run `kemory upgrade`")
	if got != want {
		t.Fatalf("report:\n%q\nwant:\n%q", got, want)
	}
	if render.CheckLabelWidth != 18 {
		t.Fatalf("CheckLabelWidth = %d, want the floor (18)", render.CheckLabelWidth)
	}
}

func TestDoctorReportNamesTheLevelEachPluginOrConnectorGives(t *testing.T) {
	tests := []struct {
		check      string
		status     render.Status
		detail     string
		wantLabel  string
		wantDetail string
	}{
		{"mcp host: codex", render.Pass, "Kemory plugin for tools, hooks from kemory-cli (/x/.codex/hooks.json)",
			"Codex", "automatic · Kemory plugin for tools, hooks from kemory-cli"},
		{"mcp host: claude-desktop", render.Pass, "connected through the claude.ai connector",
			"Claude chat (app, web, mobile)", "on demand · claude.ai connector"},
		{"mcp host: claude-code", render.Pass, "connected through the Kemory plugin (/x/settings.json)",
			"Claude Code (Code tab, terminal)", "automatic · Kemory plugin"},
		{"mcp host: grok", render.Pass, "connected through the Kemory plugin (`grok plugin list`)",
			"Grok", "on demand · Kemory plugin"},
		{"mcp host: cursor", render.Pass, "removed kemory entry — cursor is not installed",
			"Cursor", "removed kemory entry — cursor is not installed"},
		{"mcp host: warp plugin", render.Skip, "no plugin for this client",
			"Warp plugin", "no plugin for this client"},
		{"mcp host: unknown-host", render.Pass, "/x/mcp.json",
			"unknown-host", "on demand"},
	}
	for _, tt := range tests {
		t.Run(tt.check, func(t *testing.T) {
			label, detail := (&doctor{}).plain(tt.check, tt.status, tt.detail)
			if label != tt.wantLabel || detail != tt.wantDetail {
				t.Fatalf("plain = (%q, %q), want (%q, %q)", label, detail, tt.wantLabel, tt.wantDetail)
			}
		})
	}
}

func TestHostOfKeepsOnlyTheHost(t *testing.T) {
	tests := []struct{ name, raw, want string }{
		{"https URL", "https://api.kemory.s9n.ai", "api.kemory.s9n.ai"},
		{"keeps the port", "http://127.0.0.1:8080/x", "127.0.0.1:8080"},
		{"not a URL", "not a url", "not a url"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hostOf(tt.raw); got != tt.want {
				t.Fatalf("hostOf(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func passCheck(check, detail string) checkResult {
	return checkResult{Check: check, Status: string(render.Pass), Detail: &detail}
}

func problemCheck(check string, status render.Status, detail string) checkResult {
	return checkResult{Check: check, Status: string(status), Detail: &detail}
}

// roundTripOf runs checkRoundTrip against kemoryURL and returns the one check
// it recorded.
func roundTripOf(t *testing.T, kemoryURL string) checkResult {
	t.Helper()
	setHome(t)
	plainOutput(t)
	d := &doctor{}
	d.checkRoundTrip(&config.Credentials{AccessToken: "at", KemoryURL: kemoryURL}, kemoryURL)
	if len(d.checks) != 1 {
		t.Fatalf("checks = %+v, want exactly one", d.checks)
	}
	if d.checks[0].Check != "auth round-trip" {
		t.Fatalf("check = %q, want auth round-trip", d.checks[0].Check)
	}
	return d.checks[0]
}

func wantRoundTrip(t *testing.T, got checkResult, status render.Status, detail string) {
	t.Helper()
	if got.Status != string(status) {
		t.Fatalf("status = %q, want %q (detail %v)", got.Status, status, got.Detail)
	}
	if got.Detail == nil || *got.Detail != detail {
		t.Fatalf("detail = %v, want %q", got.Detail, detail)
	}
}

const wrongCellRefusal = `{"detail": {"code": "ORG_WRONG_RESIDENCY_CELL", "this_cell": "zurich"}}`

// cellServer answers /api/v1/me with status and body, counting its requests.
func cellServer(t *testing.T, status int, body string, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if status == http.StatusOK && r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// knownCells points the residency allowlist at loopback servers so a refusal
// never sends a candidate request to a real host.
func knownCells(t *testing.T, cells map[string]string) {
	t.Helper()
	old := residency.KnownCellAPIHosts
	residency.KnownCellAPIHosts = cells
	t.Cleanup(func() { residency.KnownCellAPIHosts = old })
}

func TestDoctorRoundTripExplainsA401(t *testing.T) {
	var n atomic.Int32
	srv := cellServer(t, http.StatusUnauthorized, "", &n)

	got := roundTripOf(t, srv.URL)

	wantRoundTrip(t, got, render.Fail, "401 — the server rejected a fresh token. Check that the Kemory server "+
		"trusts this identity provider and that the token's audience includes the Kemory API.")
}

func TestDoctorRoundTripPassesWhenAnotherCellConfirmsTheOrg(t *testing.T) {
	var refusedN, confirmedN atomic.Int32
	refusing := cellServer(t, http.StatusForbidden, wrongCellRefusal, &refusedN)
	confirming := cellServer(t, http.StatusOK, meBody, &confirmedN)
	knownCells(t, map[string]string{"zurich": refusing.URL, "mumbai": confirming.URL})

	got := roundTripOf(t, refusing.URL)

	wantRoundTrip(t, got, render.Pass, "org=org-1, teams=1 — served by "+confirming.URL+
		". This CLI's saved URL still points at "+refusing.URL+
		" (the 'zurich' cell); the next command switches and saves it.")
	if n := confirmedN.Load(); n != 1 {
		t.Fatalf("confirming cell got %d requests, want 1", n)
	}
}

func TestDoctorRoundTripFailsWhenNoCellConfirmsTheOrg(t *testing.T) {
	var refusedN, otherN atomic.Int32
	refusing := cellServer(t, http.StatusForbidden, wrongCellRefusal, &refusedN)
	other := cellServer(t, http.StatusForbidden, wrongCellRefusal, &otherN)
	knownCells(t, map[string]string{"zurich": refusing.URL, "mumbai": other.URL})

	got := roundTripOf(t, refusing.URL)

	wantRoundTrip(t, got, render.Fail, "403 — this org's data lives in a different residency cell (this CLI points at "+
		refusing.URL+", the 'zurich' cell). No allowlisted cell could confirm it.")
	if n := otherN.Load(); n != 1 {
		t.Fatalf("second cell got %d requests, want 1", n)
	}
}

func TestDoctorRoundTripReportsAnyOtherStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"server error", http.StatusInternalServerError, "", "GET /api/v1/me → 500"},
		{"forbidden without a residency refusal", http.StatusForbidden, `{"detail": "forbidden"}`, "GET /api/v1/me → 403"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var n atomic.Int32
			srv := cellServer(t, tt.status, tt.body, &n)
			// A refusal must never fall back to real hosts.
			knownCells(t, map[string]string{"zurich": srv.URL})

			got := roundTripOf(t, srv.URL)

			wantRoundTrip(t, got, render.Fail, tt.want)
		})
	}
}
