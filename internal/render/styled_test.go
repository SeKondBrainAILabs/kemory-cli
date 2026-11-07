package render

import (
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// strip removes colour codes, which depend on the terminal the tests run in.
func strip(s string) string { return sgr.ReplaceAllString(s, "") }

// styledTerminal makes Out and Err look like a terminal of the given width
// and restores the package state when the test ends.
func styledTerminal(t *testing.T, width int) (out, errOut *strings.Builder) {
	t.Helper()
	oldOut, oldErr := Out, Err
	oldIsTerminal, oldWidth, oldLabel := isTerminal, terminalWidth, CheckLabelWidth
	out, errOut = &strings.Builder{}, &strings.Builder{}
	Out, Err = out, errOut
	isTerminal = func(io.Writer) bool { return true }
	terminalWidth = func(io.Writer) int { return width }
	t.Setenv("NO_COLOR", "")
	t.Setenv("KEMORY_ACCESSIBLE", "")
	Configure(false, false, false)
	t.Cleanup(func() {
		Out, Err = oldOut, oldErr
		isTerminal, terminalWidth, CheckLabelWidth = oldIsTerminal, oldWidth, oldLabel
		Configure(false, false, false)
	})
	return out, errOut
}

const mcpDetail = "installed, not connected — run `kemory mcp install --host all` to wire it"

func TestStyledTableAlignsColumnsAndKeepsEveryCell(t *testing.T) {
	out, _ := styledTerminal(t, 200)
	Table("Keys", []string{"NAME", "ID"}, [][]string{{"laptop", "k_1"}, {"build-server", "k_22"}})
	want := "Keys\nNAME          ID\nlaptop        k_1\nbuild-server  k_22\n"
	if got := strip(out.String()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStyledTableNarrowsTheWidestColumnToTheTerminalWidth(t *testing.T) {
	out, _ := styledTerminal(t, 60)
	Table("", []string{"NAME", "KEY ID", "DESCRIPTION"}, [][]string{{"laptop", "k_0123456789abcdef", "What visual scale should the cannon use so it stays inside its tile?"}})
	got := strip(out.String())
	want := "NAME    KEY ID              DESCRIPTION\nlaptop  k_0123456789abcdef  What visual scale should the c…\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, l := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if lipgloss.Width(l) > 59 {
			t.Fatalf("line wider than 59 columns: %q", l)
		}
	}
}

func TestStyledTableNeverTruncatesAnIDColumn(t *testing.T) {
	out, _ := styledTerminal(t, 30)
	Table("", []string{"NAME", "KEY ID"}, [][]string{{"laptop", "k_0123456789abcdef0123456789"}})
	want := "NAME    KEY ID\nlaptop  k_0123456789abcdef0123456789\n"
	if got := strip(out.String()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStyledCheckMarksEachStatus(t *testing.T) {
	out, _ := styledTerminal(t, 200)
	Check("API", Pass, "")
	Check("API", Fail, "")
	Check("API", Skip, "")
	pad := strings.Repeat(" ", 15)
	want := "✓ API" + pad + "\n✗ API" + pad + "\n– API" + pad + "\n"
	got := strip(out.String())
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, tag := range []string{"PASS", "FAIL", "SKIP"} {
		if strings.Contains(got, tag) {
			t.Fatalf("styled check printed %s: %q", tag, got)
		}
	}
}

func TestStyledCheckWrapsDetailUnderItselfWithoutSplittingACommand(t *testing.T) {
	out, _ := styledTerminal(t, 60)
	Check("MCP", Fail, mcpDetail)
	got := strip(out.String())
	margin := strings.Repeat(" ", 22)
	want := "✗ MCP" + strings.Repeat(" ", 17) + "installed, not connected — run\n" +
		margin + "`kemory mcp install --host all` to\n" +
		margin + "wire it\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, l := range strings.Split(got, "\n") {
		if strings.Count(l, "`")%2 != 0 {
			t.Fatalf("command split across lines: %q", l)
		}
	}
}

func TestStyledCheckDoesNotWrapBelowThirtyColumns(t *testing.T) {
	out, _ := styledTerminal(t, 40)
	Check("MCP", Fail, mcpDetail)
	want := "✗ MCP" + strings.Repeat(" ", 17) + mcpDetail + "\n"
	if got := strip(out.String()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFramedStyledLinesAreIndented(t *testing.T) {
	run := func(framed bool) string {
		out, _ := styledTerminal(t, 60)
		if framed {
			Frame()
		}
		Heading("Keys")
		Table("T", []string{"NAME", "ID"}, [][]string{{"laptop", "k_1"}})
		Check("MCP", Fail, mcpDetail)
		return strip(out.String())
	}
	tail := "`kemory mcp install --host all` to\n"
	margin := func(n int) string { return strings.Repeat(" ", n) }
	wantFramed := " Keys\n T\n NAME    ID\n laptop  k_1\n ✗ MCP" + margin(17) + "installed, not connected — run\n" +
		margin(23) + tail + margin(23) + "wire it\n"
	wantPlain := "Keys\nT\nNAME    ID\nlaptop  k_1\n✗ MCP" + margin(17) + "installed, not connected — run\n" +
		margin(22) + tail + margin(22) + "wire it\n"
	if got := run(true); got != wantFramed {
		t.Fatalf("framed: got %q, want %q", got, wantFramed)
	}
	if got := run(false); got != wantPlain {
		t.Fatalf("unframed: got %q, want %q", got, wantPlain)
	}
}

func TestStyledMessagesUseMarks(t *testing.T) {
	out, errOut := styledTerminal(t, 80)
	Accent("Next steps")
	Success("✓ done")
	Warn("✗ slow")
	Error("boom")
	Heading("Keys")
	Muted("hint")
	if got, want := strip(errOut.String()), "Next steps\n⚠ slow\n✗ boom\n"; got != want {
		t.Fatalf("stderr: got %q, want %q", got, want)
	}
	if got, want := strip(out.String()), "✓ done\nKeys\nhint\n"; got != want {
		t.Fatalf("stdout: got %q, want %q", got, want)
	}

	Configure(false, false, true)
	before := out.String()
	Muted("hint")
	Success("x")
	if out.String() != before {
		t.Fatalf("quiet mode still printed: %q", strip(out.String()[len(before):]))
	}
}

func TestStyledKVsWrapsALongValueUnderItself(t *testing.T) {
	out, _ := styledTerminal(t, 50)
	KVs([]KV{{Key: "Email", Value: "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa quebec romeo sierra tango"}})
	got := strip(out.String())
	margin := strings.Repeat(" ", 7)
	want := "Email  alpha bravo charlie delta echo foxtrot golf\n" +
		margin + "hotel india juliet kilo lima mike november\n" +
		margin + "oscar papa quebec romeo sierra tango\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, l := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if lipgloss.Width(l) > 50 {
			t.Fatalf("line wider than 50 columns: %q", l)
		}
	}
}
