package ui

import (
	"strings"
	"testing"
)

func TestGradientEndsAtItsStops(t *testing.T) {
	stops := []rgb{hex(Blue), hex(Purple), hex(Pink)}
	if gradient(stops, 0).String() != "#1A8AF6" || gradient(stops, 1).String() != "#E24AF2" || gradient(stops, 0.5).String() != "#8B78F5" {
		t.Fatalf("got %s %s %s", gradient(stops, 0), gradient(stops, 0.5), gradient(stops, 1))
	}
}

func TestHeaderHasTheMarkNameTaglineAndMaker(t *testing.T) {
	h := Header()
	lines := strings.Split(strings.TrimSuffix(h, "\n"), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "◉") || !strings.Contains(lines[1], "persistent memory for your AI agents") ||
		!strings.Contains(lines[2], "by SeKondBrain") {
		t.Fatalf("the mark sits in line with the name, over the tagline and maker:\n%s", h)
	}
}

func TestEveryHostHasAName(t *testing.T) {
	for host, name := range names {
		if Name(host) != name {
			t.Fatalf("%s: %q", host, Name(host))
		}
	}
	if Name("unknown") != "unknown" {
		t.Fatal("an unknown host keeps its id")
	}
}
