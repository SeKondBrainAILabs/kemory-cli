package cli

import (
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

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
