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
		if newScreen(g) != nil {
			t.Fatalf("%s must get plain output", name)
		}
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
	for in, want := range map[string]string{"": "", "A": "A", "A,B": "A and B", "A,B,C": "A, B and C"} {
		var names []string
		if in != "" {
			names = strings.Split(in, ",")
		}
		if got := listJoin(names); got != want {
			t.Fatalf("listJoin(%q) = %q, want %q", in, got, want)
		}
	}
}
