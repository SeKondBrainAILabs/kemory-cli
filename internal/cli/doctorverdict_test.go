package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/version"
)

const (
	verdictFixHint = "Run kemory doctor --fix to connect every installed client and clean up the rest"
	verdictTail    = ": each ✗ above says what to do"
)

// verdictSetup builds the healthy setup of TestDoctorHealthySetup against a
// fake release 0.6.14, optionally with Claude Code wired and Cursor installed
// but not wired, and pins the CLI version.
func verdictSetup(t *testing.T, claude, cursor bool, ver string) {
	t.Helper()
	home := setHome(t)
	fakeReleases(t, `[{"tag_name": "cli-v0.6.14", "assets": [{"name": "kemory-linux-x64.tar.gz"}]}]`)
	api := fakeKemory(t)
	issuer := fakeIssuer(t)
	c := &config.Credentials{AccessToken: "at", RefreshToken: "rt", ExpiresAt: 1, Issuer: issuer.URL, ClientID: "kemory-cli", KemoryURL: api.URL, Env: "prod"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if claude {
		installApp(t, home, ".claude")
		run(t, "mcp", "install")
	}
	if cursor {
		installApp(t, home, ".cursor")
	}
	old := version.Version
	version.Version = ver
	t.Cleanup(func() { version.Version = old })
}

// styledDoctor runs `kemory doctor` with stdout standing in for a terminal and
// returns what it printed, without colour codes. Stderr is not a terminal, so
// no spinner starts.
func styledDoctor(t *testing.T) string {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	oldOut, oldErr, oldTerm := render.Out, render.Err, render.IsTerminal
	render.Out, render.Err = &outBuf, &errBuf
	render.IsTerminal = func(w io.Writer) bool { return w == io.Writer(&outBuf) }
	t.Cleanup(func() { render.Out, render.Err, render.IsTerminal = oldOut, oldErr, oldTerm })
	t.Setenv("KEMORY_ACCESSIBLE", "")
	Execute([]string{"doctor"})
	return sgrCodes.ReplaceAllString(outBuf.String(), "")
}

// requirePremise fails with the whole report unless the JSON report has
// exactly the named failing and skipped checks.
func requirePremise(t *testing.T, fails, skips []string) {
	t.Helper()
	_, report := runDoctor(t)
	var gotFails, gotSkips []string
	for _, c := range report.Checks {
		switch c.Status {
		case "fail":
			gotFails = append(gotFails, c.Check)
		case "skip":
			gotSkips = append(gotSkips, c.Check)
		}
	}
	if strings.Join(gotFails, ",") != strings.Join(fails, ",") || strings.Join(gotSkips, ",") != strings.Join(skips, ",") {
		t.Fatalf("premise: want fails %v skips %v, got fails %v skips %v\n%+v", fails, skips, gotFails, gotSkips, report)
	}
}

func requireContains(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

func requireLacks(t *testing.T, out string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(out, w) {
			t.Errorf("output has %q:\n%s", w, out)
		}
	}
}

func TestDoctorStyledVerdictCountsOneProblemAndShowsTheFixHint(t *testing.T) {
	verdictSetup(t, true, true, "0.6.13")
	requirePremise(t, []string{"cli up-to-date"}, []string{"mcp host: cursor"})
	out := styledDoctor(t)
	requireContains(t, out, "✗ 1 problem"+verdictTail, verdictFixHint)
	requireLacks(t, out, "1 problems", "Everything works")
	if strings.Index(out, verdictFixHint) > strings.Index(out, "✗ 1 problem") {
		t.Errorf("fix hint should come before the verdict:\n%s", out)
	}
}

func TestDoctorStyledVerdictCountsTwoProblems(t *testing.T) {
	verdictSetup(t, false, false, "0.6.13")
	requirePremise(t, []string{"mcp host config", "cli up-to-date"}, nil)
	out := styledDoctor(t)
	requireContains(t, out, "✗ 2 problems"+verdictTail, verdictFixHint)
	requireLacks(t, out, "2 problem:", "Everything works")
}

func TestDoctorStyledVerdictSaysEverythingWorksWithOneCheckSkipped(t *testing.T) {
	verdictSetup(t, true, false, "dev")
	requirePremise(t, nil, []string{"cli up-to-date"})
	out := styledDoctor(t)
	requireContains(t, out, "✓ Everything works · 1 check skipped")
	requireLacks(t, out, "checks skipped", "problem", "kemory doctor --fix")
}

func TestDoctorStyledVerdictCountsTwoSkippedChecksAndShowsTheFixHint(t *testing.T) {
	verdictSetup(t, true, true, "dev")
	requirePremise(t, nil, []string{"mcp host: cursor", "cli up-to-date"})
	out := styledDoctor(t)
	requireContains(t, out, "✓ Everything works · 2 checks skipped", verdictFixHint)
	requireLacks(t, out, "2 check skipped", "problem")
}

func TestDoctorStyledVerdictSaysEverythingWorksWhenAllPass(t *testing.T) {
	verdictSetup(t, true, false, "0.6.14")
	requirePremise(t, nil, nil)
	out := styledDoctor(t)
	found := false
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "✓ Everything works" {
			found = true
		}
	}
	if !found {
		t.Errorf("no line is exactly the verdict:\n%s", out)
	}
	requireLacks(t, out, "skipped", "problem", "kemory doctor --fix")
}
