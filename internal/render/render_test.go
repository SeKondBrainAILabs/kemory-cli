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
