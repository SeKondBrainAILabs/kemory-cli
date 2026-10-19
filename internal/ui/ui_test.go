package ui

import (
	"strings"
	"testing"
)

func TestHeaderDrawsTheMarkOnlyWhereImagesWork(t *testing.T) {
	old := images
	t.Cleanup(func() { images = old })

	images = noImages
	h := Header()
	for _, want := range []string{"persistent memory for your AI agents", "by SeKondBrain"} {
		if !strings.Contains(h, want) {
			t.Fatalf("missing %q in:\n%s", want, h)
		}
	}
	if strings.Contains(h, "\x1b]1337") || strings.Count(h, "\n") != 3 {
		t.Fatalf("plain header:\n%q", h)
	}

	images = itermImages
	h = Header()
	if !strings.Contains(h, "width=6;height=3") || !strings.HasSuffix(h, "\x1b8\x1b[3B\r") {
		t.Fatalf("image header: %.80q", h)
	}
}

func TestGradientEndsAtItsStops(t *testing.T) {
	stops := []rgb{hex(Blue), hex(Purple), hex(Pink)}
	if gradient(stops, 0).String() != "#1A8AF6" || gradient(stops, 1).String() != "#E24AF2" || gradient(stops, 0.5).String() != "#8B78F5" {
		t.Fatalf("got %s %s %s", gradient(stops, 0), gradient(stops, 0.5), gradient(stops, 1))
	}
}

func TestEveryClientHasANameTileAndLogo(t *testing.T) {
	for host, c := range clients {
		if Name(host) != c.name || !strings.Contains(tile(host), c.glyph) {
			t.Fatalf("%s: %q %q", host, Name(host), tile(host))
		}
		if _, err := iconFiles.ReadFile("icons/" + host + ".png"); err != nil {
			t.Fatalf("%s has no logo: %v", host, err)
		}
	}
	if Name("unknown") != "unknown" || !strings.Contains(tile("unknown"), "•") {
		t.Fatal("an unknown host still gets a name and a tile")
	}
}

func TestImagesOnlyWhereTheTerminalDrawsThem(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want imageProtocol
	}{
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, itermImages},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, itermImages},
		{map[string]string{"TERM": "xterm-kitty"}, kittyImages},
		{map[string]string{"TERM": "xterm-ghostty"}, kittyImages},
		{map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, noImages},
		{map[string]string{"TERM_PROGRAM": "WarpTerminal"}, noImages},
		{map[string]string{"TERM_PROGRAM": "WarpTerminal", "KEMORY_ICONS": "1"}, itermImages},
		{map[string]string{"TERM_PROGRAM": "iTerm.app", "TMUX": "/tmp/t"}, noImages},
		{map[string]string{"TERM_PROGRAM": "iTerm.app", "KEMORY_ICONS": "0"}, noImages},
	}
	for _, c := range cases {
		if got := detectImages(func(k string) string { return c.env[k] }); got != c.want {
			t.Fatalf("%v: got %d, want %d", c.env, got, c.want)
		}
	}
}

func TestLogoSpeaksEachProtocol(t *testing.T) {
	old := images
	t.Cleanup(func() { images = old })

	images = itermImages
	if l := Logo("cursor"); !strings.HasPrefix(l, "\x1b]1337;File=inline=1;width=2;height=1;") || !strings.HasSuffix(l, "\a") {
		t.Fatalf("iTerm2: %.60q", l)
	}
	images = kittyImages
	l := Logo("claude-desktop") // over 4096 base64 bytes, so chunked
	if !strings.HasPrefix(l, "\x1b_Ga=T,f=100,c=2,r=1,q=2,m=1;") || !strings.Contains(l, "\x1b_Gm=0;") {
		t.Fatalf("kitty: %.60q", l)
	}
	images = noImages
	if Logo("cursor") != tile("cursor") {
		t.Fatal("without images the tile is drawn")
	}
}
