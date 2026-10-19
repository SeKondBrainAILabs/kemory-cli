package render

import (
	"strings"
	"testing"
)

func TestWrapNeverSplitsABacktickedCommand(t *testing.T) {
	lines := WrapKeepingCommands("installed, not connected — run `kemory mcp install --host all` to wire it", 20)
	for _, l := range lines {
		if strings.Count(l, "`")%2 != 0 {
			t.Fatalf("command split across lines: %q", lines)
		}
	}
	if strings.Join(lines, " ") != "installed, not connected — run `kemory mcp install --host all` to wire it" {
		t.Fatalf("text changed: %q", lines)
	}
}

func TestWrapRespectsWidth(t *testing.T) {
	for _, l := range WrapKeepingCommands("one two three four five six seven", 10) {
		if len(l) > 10 {
			t.Fatalf("line too long: %q", l)
		}
	}
}

func TestTruncateEndsInAnEllipsisWithinWidth(t *testing.T) {
	got := truncate("What visual scale should the cannon use so it stays inside its tile?", 20)
	if len([]rune(got)) > 20 || !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q", got)
	}
}

func TestPlainOutputIsUnchangedOutsideATerminal(t *testing.T) {
	var out strings.Builder
	old := Out
	Out = &out
	t.Cleanup(func() { Out = old })
	Configure(false, false, false)
	Frame()
	Success("✓ done")
	KVs([]KV{{Key: "Email", Value: "a@b.c"}})
	if got := out.String(); got != "✓ done\nEmail  a@b.c\n" {
		t.Fatalf("got %q", got)
	}
}
