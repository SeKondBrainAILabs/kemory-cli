package cli

import (
	"bytes"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/ui"
)

var ansiSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// captureScreenOutput routes render's stdout and stderr to buffers for the
// test and returns a function giving what was printed to stdout, without
// colour codes.
func captureScreenOutput(t *testing.T) func() string {
	t.Helper()
	var out, errOut bytes.Buffer
	render.Out, render.Err = &out, &errOut
	render.Configure(false, false, false)
	t.Cleanup(func() {
		render.Out, render.Err = os.Stdout, os.Stderr
		render.Configure(false, false, false)
	})
	return func() string { return ansiSGR.ReplaceAllString(out.String(), "") }
}

func TestTheGuidedScreenOnlyRunsInAColourTerminal(t *testing.T) {
	setHome(t)
	t.Setenv("NO_COLOR", "")
	t.Setenv("KEMORY_ACCESSIBLE", "")
	terminalWidth = func() int { return 120 }
	render.Configure(false, false, false)
	t.Cleanup(func() { render.Configure(false, false, false) })

	if newScreen(&globals{}) == nil {
		t.Fatal("an interactive colour terminal gets the guided screen")
	}
	for name, g := range map[string]*globals{"--quiet": {quiet: true}, "--no-color": {noColor: true}} {
		t.Run(name, func(t *testing.T) {
			if newScreen(g) != nil {
				t.Fatalf("%s must get plain output", name)
			}
		})
	}
	t.Setenv("NO_COLOR", "1")
	if newScreen(&globals{}) != nil {
		t.Fatal("NO_COLOR must get plain output")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("KEMORY_ACCESSIBLE", "1")
	if newScreen(&globals{}) != nil {
		t.Fatal("KEMORY_ACCESSIBLE=1 must get the plain numbered prompt")
	}
	t.Setenv("KEMORY_ACCESSIBLE", "")
	render.Configure(false, true, false)
	if newScreen(&globals{}) != nil {
		t.Fatal("--output json must get plain output")
	}
	render.Configure(false, false, false)
	terminalWidth = func() int { return 0 }
	if newScreen(&globals{}) != nil {
		t.Fatal("no terminal, no guided screen")
	}
}

func TestListJoin(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"none", "", ""},
		{"one", "A", "A"},
		{"two", "A,B", "A and B"},
		{"three", "A,B,C", "A, B and C"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var names []string
			if c.in != "" {
				names = strings.Split(c.in, ",")
			}
			if got := listJoin(names); got != c.want {
				t.Fatalf("listJoin(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestResultsCountOnlyNewConnectionsAndSummaryListsEachNextStepOnce(t *testing.T) {
	setHome(t)
	printed := captureScreenOutput(t)
	s := &screen{width: 120}

	s.results([]hostResult{
		{host: "cursor", ok: true, short: "connected", next: "Open Cursor and enable Kemory"},
		{host: "codex", ok: true, short: "already connected"},
		{host: "vscode", ok: false, detail: "permission denied"},
		{host: "copilot-jetbrains", ok: true, short: "connected", next: "Open Cursor and enable Kemory", restart: true},
		{host: "junie", ok: true, short: "connected", restart: true},
		{host: "claude-desktop", ok: true, short: "connected", restart: true},
	})
	if s.connected != 4 {
		t.Fatalf("connected = %d, want 4: an already connected client and a failed one are not new connections", s.connected)
	}
	s.summary("ada@example.com")
	got := printed()

	if !strings.Contains(got, "Kemory is connected to 4 clients for ada@example.com") {
		t.Errorf("headline count missing:\n%s", got)
	}
	if n := strings.Count(got, "Open Cursor and enable Kemory"); n != 1 {
		t.Errorf("a repeated next step is printed %d times, want once:\n%s", n, got)
	}
	if !strings.Contains(got, "permission denied") {
		t.Errorf("a failed row shows its detail when it has no short status:\n%s", got)
	}
	const restart = "Restart your JetBrains IDE and Claude to load Kemory"
	if n := strings.Count(got, "Restart "); n != 1 || !strings.Contains(got, restart) {
		t.Errorf("want one line %q, got %d restart lines:\n%s", restart, n, got)
	}
	doctor := strings.Index(got, "kemory doctor")
	if doctor < 0 || doctor < strings.Index(got, restart) {
		t.Errorf("the doctor step must follow the restart line:\n%s", got)
	}
}

func TestSummaryWithNothingNewlyConnectedOmitsTheCount(t *testing.T) {
	setHome(t)
	printed := captureScreenOutput(t)
	s := &screen{width: 120}

	s.results([]hostResult{
		{host: "codex", ok: true, short: "already connected"},
		{host: "vscode", ok: false, detail: "permission denied"},
	})
	s.summary("ada@example.com")
	got := printed()

	if !strings.Contains(got, "Kemory is connected for ada@example.com") {
		t.Errorf("headline without a count missing:\n%s", got)
	}
	if strings.Contains(got, "connected to") {
		t.Errorf("no count when nothing is new:\n%s", got)
	}
	if strings.Contains(got, "Restart") {
		t.Errorf("no restart line when no row asked for one:\n%s", got)
	}
}

func TestSummaryCountsOneClientInTheSingular(t *testing.T) {
	setHome(t)
	printed := captureScreenOutput(t)
	s := &screen{width: 120}

	s.results([]hostResult{{host: "cursor", ok: true, short: "connected"}})
	s.summary("x")

	if got := printed(); !strings.Contains(got, "connected to 1 client for x") {
		t.Fatalf("want the singular headline:\n%s", got)
	}
}

func TestSignedInNamesAccountOrgAndNonDefaultServer(t *testing.T) {
	setHome(t)
	printed := captureScreenOutput(t)
	srv := fakeKemory(t)
	saveCreds(t, srv.URL)
	creds, err := config.LoadCredentials("prod")
	if err != nil || creds == nil {
		t.Fatalf("load credentials: %v, %v", creds, err)
	}
	if creds.Email == "" {
		creds.Email = "ada@example.com"
	}
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	s := &screen{width: 120}

	s.signedIn(&globals{env: config.DefaultEnv}, creds)
	first := printed()
	if !strings.HasPrefix(first, "┌") || !strings.Contains(first, "Signed in as ada@example.com") || !strings.Contains(first, "· Acme") {
		t.Errorf("first line opens the rail with the account and org:\n%s", first)
	}
	if strings.Contains(first, u.Host) {
		t.Errorf("the default server is not named:\n%s", first)
	}
	if !s.railOpen {
		t.Error("signedIn must open the rail")
	}

	s.signedIn(&globals{env: "staging"}, creds)
	second := strings.TrimPrefix(printed(), first)
	if !strings.HasPrefix(second, "│") || !strings.Contains(second, u.Host) {
		t.Errorf("a later line stays on the rail and names a non-default server %s:\n%s", u.Host, second)
	}
}

func TestOrgNameForFallsBackToTheOrgListAndToEmpty(t *testing.T) {
	setHome(t)
	srv := fakeAPI(t, `[{"org_id": "org-1", "name": "Org One"}]`)
	saveCreds(t, srv.URL)
	creds, err := config.LoadCredentials("prod")
	if err != nil || creds == nil {
		t.Fatalf("load credentials: %v, %v", creds, err)
	}
	if got := orgNameFor(creds); got != "Org One" {
		t.Errorf("org name from the org list = %q, want %q", got, "Org One")
	}

	creds.KemoryURL = "http://127.0.0.1:1"
	if got := orgNameFor(creds); got != "" {
		t.Errorf("unreachable server gives %q, want no name", got)
	}
}

func TestPickFancyWithNoReadyClientSaysEveryClientHasKemory(t *testing.T) {
	setHome(t)
	printed := captureScreenOutput(t)
	s := &screen{width: 120}

	picked, err := s.pickFancy(
		[]pickRow{{host: "cursor", state: "connected", mark: ui.Connected, short: "connected"}},
		[]string{"codex", "vscode"},
	)
	if picked != nil || err != nil {
		t.Fatalf("pickFancy = %v, %v, want nil, nil without asking", picked, err)
	}
	got := printed()
	if want := "Not installed: " + ui.ShortName("codex") + ", " + ui.ShortName("vscode"); !strings.Contains(got, want) {
		t.Errorf("want %q in:\n%s", want, got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "Every AI client here already has Kemory.") {
		t.Errorf("output must end with the closing line:\n%s", got)
	}
}
