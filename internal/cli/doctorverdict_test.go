package cli

import (
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

const fixHintLine = "Run kemory doctor --fix to connect every installed client and clean up the rest"

func TestDoctorVerdictLine(t *testing.T) {
	pass := passCheck("kemory reachable", "ok")
	skip := checkResult{Check: "cli up-to-date", Status: string(render.Skip)}
	fail := checkResult{Check: "token refresh", Status: string(render.Fail)}

	for _, c := range []struct {
		name    string
		checks  []checkResult
		fixHint bool
		want    string
		absent  []string
	}{
		{"one failure is singular", []checkResult{pass, fail}, false, "✗ 1 problem: each ✗ above says what to do", []string{"Everything works", "problems"}},
		{"two failures are plural", []checkResult{fail, pass, fail}, false, "✗ 2 problems: each ✗ above says what to do", []string{"Everything works"}},
		{"a failure outranks skips", []checkResult{skip, fail, skip}, false, "✗ 1 problem: each ✗ above says what to do", []string{"Everything works", "skipped"}},
		{"one skip is singular", []checkResult{pass, skip}, false, "✓ Everything works · 1 check skipped", []string{"problem", "checks skipped"}},
		{"two skips are plural", []checkResult{skip, pass, skip}, false, "✓ Everything works · 2 checks skipped", []string{"problem"}},
		{"all passes say nothing about skips", []checkResult{pass, pass}, false, "✓ Everything works\n", []string{"skipped", "problem"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := plainOutput(t)
			(&doctor{checks: c.checks}).verdict(c.fixHint)
			got := sgrCodes.ReplaceAllString(out.String(), "")
			if !strings.Contains(got, c.want) {
				t.Fatalf("missing %q in:\n%s", c.want, got)
			}
			for _, s := range append(c.absent, "kemory doctor --fix") {
				if strings.Contains(got, s) {
					t.Fatalf("%q should not appear in:\n%s", s, got)
				}
			}
		})
	}
}

func TestDoctorVerdictShowsTheFixHintOnlyWhenAsked(t *testing.T) {
	fail := checkResult{Check: "mcp host config", Status: string(render.Fail)}
	for _, c := range []struct {
		name   string
		checks []checkResult
		want   string
	}{
		{"with a failure", []checkResult{fail}, "1 problem"},
		{"with only passes", []checkResult{passCheck("kemory reachable", "ok")}, "Everything works"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := plainOutput(t)
			(&doctor{checks: c.checks}).verdict(true)
			got := sgrCodes.ReplaceAllString(out.String(), "")
			hint := strings.Index(got, fixHintLine)
			verdict := strings.Index(got, c.want)
			if hint < 0 || verdict < 0 || hint > verdict {
				t.Fatalf("want the --fix hint before %q, got:\n%s", c.want, got)
			}
		})
	}
}
