package ui

import (
	"strings"
	"testing"
)

func TestBannerHasTheWordmarkAndTagline(t *testing.T) {
	b := Banner()
	if strings.Count(b, "\n") != len(banner)+1 || !strings.Contains(b, "persistent memory for your AI agents") {
		t.Fatalf("banner:\n%s", b)
	}
}

func TestGradientEndsAtItsStops(t *testing.T) {
	stops := []rgb{hex(Blue), hex(Purple), hex(Pink)}
	if gradient(stops, 0).String() != "#1A8AF6" || gradient(stops, 1).String() != "#E24AF2" || gradient(stops, 0.5).String() != "#8B78F5" {
		t.Fatalf("got %s %s %s", gradient(stops, 0), gradient(stops, 0.5), gradient(stops, 1))
	}
}

func TestEveryClientHasANameAndIcon(t *testing.T) {
	for host, c := range clients {
		if Name(host) != c.name || !strings.Contains(Icon(host), c.glyph) {
			t.Fatalf("%s: %q %q", host, Name(host), Icon(host))
		}
	}
	if Name("unknown") != "unknown" || !strings.Contains(Icon("unknown"), "•") {
		t.Fatal("an unknown host still gets a name and an icon")
	}
}

func TestSummaryListsEveryStep(t *testing.T) {
	s := Summary("Kemory is connected", []string{"Restart Cursor", "Run /hooks in Codex"})
	for _, want := range []string{"Kemory is connected", "Restart Cursor", "Run /hooks in Codex", "╭", "╯"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}
