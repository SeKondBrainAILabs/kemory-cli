// Package render is the only place the CLI prints from. It applies the
// presentation flags: colour (off without a terminal or with NO_COLOR), JSON
// mode (stdout carries only the payload) and --quiet (drop confirmations and
// hints).
package render

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Out and Err are where output goes. Overridable in tests.
var (
	Out io.Writer = os.Stdout
	Err io.Writer = os.Stderr
)

var (
	noColor  bool
	jsonMode bool
	quiet    bool
)

// Configure records the CLI-level presentation flags.
func Configure(disableColor, json, q bool) {
	noColor = disableColor || os.Getenv("NO_COLOR") != ""
	jsonMode = json
	quiet = q
	framed = false
}

// JSONMode reports whether the caller asked for machine-readable output.
func JSONMode() bool { return jsonMode }

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

const (
	styleMuted   = "2"
	styleAccent  = "36"
	styleSuccess = "32"
	styleWarn    = "33"
	styleError   = "31"
	styleBold    = "1"
)

// In a colour terminal output takes the guided installer's look: Kemory's
// colours, ✓ ✗ – marks and dimmed secondary text. lipgloss degrades the
// colours to what the terminal supports. Everywhere else, pipes and JSON
// mode included, the plain text below is unchanged.
var (
	tintMint   = lipgloss.NewStyle().Foreground(lipgloss.Color("#3DD680")).Bold(true)
	tintOrange = lipgloss.NewStyle().Foreground(lipgloss.Color("#F28B68")).Bold(true)
	tintTitle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B78F5")).Bold(true)
	tintDim    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "243"})
	tintBold   = lipgloss.NewStyle().Bold(true)
)

// styled reports whether w gets the full look.
func styled(w io.Writer) bool {
	return !noColor && isTerminal(w) && os.Getenv("KEMORY_ACCESSIBLE") != "1"
}

// Styled reports whether human output on stdout gets the full look.
func Styled() bool { return !jsonMode && styled(stdout()) }

// framed is set once a command has printed its header: from then on every
// line it prints is indented to sit under the mark.
var framed bool

// Frame indents the rest of the output under a header the caller printed.
func Frame() { framed = true }

// indent is the margin a framed, styled line gets.
func indent(w io.Writer) string {
	if framed && styled(w) {
		return " "
	}
	return ""
}

// indentLines puts the margin before every line of text.
func indentLines(w io.Writer, text string) string {
	m := indent(w)
	if m == "" {
		return text
	}
	return m + strings.ReplaceAll(text, "\n", "\n"+m)
}

func paint(w io.Writer, style, text string) string {
	if noColor || style == "" || !isTerminal(w) {
		return text
	}
	return "\x1b[" + style + "m" + text + "\x1b[0m"
}

// stdout is Out, except in JSON mode, where human-facing text moves to Err so
// stdout stays parseable.
func stdout() io.Writer {
	if jsonMode {
		return Err
	}
	return Out
}

// Echo prints one line to stdout.
func Echo(text string) {
	w := stdout()
	if text == "" {
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintln(w, indentLines(w, text))
}

// Accent prints one highlighted line to stderr.
func Accent(text string) {
	if styled(Err) {
		fmt.Fprintln(Err, tintTitle.Render(text))
		return
	}
	fmt.Fprintln(Err, paint(Err, styleAccent, text))
}

// Muted prints secondary context. Dropped by --quiet.
func Muted(text string) {
	if quiet {
		return
	}
	w := stdout()
	if styled(w) {
		fmt.Fprintln(w, indent(w)+tintDim.Render(text))
		return
	}
	fmt.Fprintln(w, paint(w, styleMuted, text))
}

// Success confirms something that just happened. Dropped by --quiet.
func Success(text string) {
	if quiet {
		return
	}
	w := stdout()
	if styled(w) {
		fmt.Fprintln(w, indent(w)+tintMint.Render("✓")+" "+strings.TrimPrefix(text, "✓ "))
		return
	}
	fmt.Fprintln(w, paint(w, styleSuccess, text))
}

// Warn is advisory, not a failure. Always stderr.
func Warn(text string) {
	if styled(Err) {
		fmt.Fprintln(Err, indent(Err)+tintOrange.Render("⚠")+" "+strings.TrimPrefix(strings.TrimPrefix(text, "⚠ "), "✗ "))
		return
	}
	fmt.Fprintln(Err, paint(Err, styleWarn, text))
}

// Error prints a failure message to stderr in the "Error: ..." shape.
func Error(text string) {
	if styled(Err) {
		fmt.Fprintln(Err, indent(Err)+tintOrange.Render("✗")+" "+text)
		return
	}
	fmt.Fprintln(Err, paint(Err, styleError, "Error: ")+text)
}

// JSON writes a payload to stdout, unchanged by any flag.
func JSON(payload []byte) {
	_, _ = Out.Write(payload)
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		fmt.Fprintln(Out)
	}
}

// KV is one key/value pair for KV.
type KV struct {
	Key   string
	Value string
}

// KVs prints aligned "key  value" lines.
func KVs(pairs []KV) {
	pad := 0
	for _, p := range pairs {
		pad = max(pad, len(p.Key))
	}
	w := stdout()
	for _, p := range pairs {
		key := p.Key + strings.Repeat(" ", pad-len(p.Key))
		if styled(w) {
			// A long value wraps under itself, not back at column 0.
			margin := len(indent(w)) + pad + 2
			lines := []string{p.Value}
			if width := terminalWidth(w); width-margin >= 30 && len(p.Value) > width-margin && !strings.Contains(p.Value, "\x1b") {
				lines = WrapKeepingCommands(p.Value, width-margin)
			}
			fmt.Fprintf(w, "%s%s  %s\n", indent(w), tintDim.Render(key), lines[0])
			for _, l := range lines[1:] {
				fmt.Fprintln(w, strings.Repeat(" ", margin)+l)
			}
			continue
		}
		fmt.Fprintf(w, "%s  %s\n", paint(w, styleMuted, key), p.Value)
	}
}

// Table prints rows under a title and column headers. With no rows it prints
// nothing: callers say "none" in words instead.
func Table(title string, columns []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	w := stdout()
	if styled(w) {
		styledTable(w, title, columns, rows)
		return
	}
	if title != "" {
		fmt.Fprintln(w, paint(w, styleBold, title))
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(columns, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}

// styledTable pads by display width, since colour codes would throw out
// tabwriter's byte counts, and dims the column headers.
func styledTable(w io.Writer, title string, columns []string, rows [][]string) {
	widths := make([]int, len(columns))
	for i, c := range columns {
		widths[i] = lipgloss.Width(c)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) {
				widths[i] = max(widths[i], lipgloss.Width(cell))
			}
		}
	}
	// A table wider than the terminal narrows its widest columns evenly, their
	// text ending in …, instead of wrapping back to column 0. ID columns keep
	// their width: an ID is copied whole into another command.
	if tw := terminalWidth(w); tw > 0 && len(widths) > 0 {
		total := len(indent(w)) + 2*(len(widths)-1)
		for _, cw := range widths {
			total += cw
		}
		for total > tw-1 {
			widest := -1
			for i, cw := range widths {
				if strings.EqualFold(columns[i], "ID") || cw <= 12 {
					continue
				}
				if widest < 0 || cw > widths[widest] {
					widest = i
				}
			}
			if widest < 0 {
				break
			}
			widths[widest]--
			total--
		}
	}
	line := func(cells []string, tint lipgloss.Style) {
		var b strings.Builder
		cells = append([]string{}, cells...)
		for i := range cells {
			if i < len(widths) && lipgloss.Width(cells[i]) > widths[i] {
				cells[i] = truncate(cells[i], widths[i])
			}
		}
		for i, cell := range cells {
			if i < len(cells)-1 && i < len(widths) {
				cell += strings.Repeat(" ", max(0, widths[i]-lipgloss.Width(cell))+2)
			}
			b.WriteString(tint.Render(cell))
		}
		fmt.Fprintln(w, indent(w)+b.String())
	}
	if title != "" {
		fmt.Fprintln(w, indent(w)+tintTitle.Render(title))
	}
	line(columns, tintDim)
	for _, row := range rows {
		line(row, lipgloss.NewStyle())
	}
}

// truncate shortens text to width display columns, ending in ….
func truncate(text string, width int) string {
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return strings.TrimRight(string(runes), " ") + "…"
}

// Heading prints a title line.
func Heading(text string) {
	w := stdout()
	if styled(w) {
		fmt.Fprintln(w, indent(w)+tintTitle.Render(text))
		return
	}
	fmt.Fprintln(w, paint(w, styleBold, text))
}

// Status is the outcome of one check.
type Status string

// Check outcomes.
const (
	Pass Status = "pass"
	Fail Status = "fail"
	Skip Status = "skip"
)

const checkLabelWidth = 26

// Check prints one "PASS  label  detail" line. On a terminal, a long detail
// wraps under the detail column instead of at column 0, and never inside a
// `backticked` command, which would paste as two broken commands.
func Check(label string, status Status, detail string) {
	tag, style := "SKIP", styleWarn
	switch status {
	case Pass:
		tag, style = "PASS", styleSuccess
	case Fail:
		tag, style = "FAIL", styleError
	}
	w := stdout()
	if styled(w) {
		styledCheck(w, label, status, detail)
		return
	}
	line := paint(w, style, tag) + "  " + padRight(label, checkLabelWidth)
	if detail == "" {
		fmt.Fprintln(w, strings.TrimRight(line, " "))
		return
	}
	indent := len(tag) + 2 + checkLabelWidth + 2
	lines := []string{detail}
	if width := terminalWidth(w); width-indent >= 30 {
		lines = WrapKeepingCommands(detail, width-indent)
	}
	fmt.Fprintln(w, line+"  "+lines[0])
	for _, l := range lines[1:] {
		fmt.Fprintln(w, strings.Repeat(" ", indent)+l)
	}
}

// styledCheck is a check as a mark, the label in bold and the detail
// dimmed, wrapped under itself.
func styledCheck(w io.Writer, label string, status Status, detail string) {
	mark := tintDim.Render("–")
	switch status {
	case Pass:
		mark = tintMint.Render("✓")
	case Fail:
		mark = tintOrange.Render("✗")
	}
	head := indent(w) + mark + " " + tintBold.Render(padRight(label, checkLabelWidth))
	if detail == "" {
		fmt.Fprintln(w, head)
		return
	}
	margin := len(indent(w)) + 1 + 1 + checkLabelWidth + 2
	lines := []string{detail}
	if width := terminalWidth(w); width-margin >= 30 {
		lines = WrapKeepingCommands(detail, width-margin)
	}
	fmt.Fprintln(w, head+"  "+tintDim.Render(lines[0]))
	for _, l := range lines[1:] {
		fmt.Fprintln(w, strings.Repeat(" ", margin)+tintDim.Render(l))
	}
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func terminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return 0
	}
	width, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return width
}

// WrapKeepingCommands wraps text to width at spaces, treating each
// `backticked` span as one word so a command is never split across lines.
func WrapKeepingCommands(text string, width int) []string {
	var words []string
	for len(text) > 0 {
		text = strings.TrimLeft(text, " ")
		if text == "" {
			break
		}
		end := strings.IndexByte(text, ' ')
		if open := strings.IndexByte(text, '`'); open >= 0 && (end < 0 || open < end) {
			if closing := strings.IndexByte(text[open+1:], '`'); closing >= 0 {
				after := open + 1 + closing + 1
				if next := strings.IndexByte(text[after:], ' '); next >= 0 {
					end = after + next
				} else {
					end = -1
				}
			}
		}
		if end < 0 {
			words = append(words, text)
			break
		}
		words = append(words, text[:end])
		text = text[end:]
	}
	var lines []string
	current := ""
	for _, word := range words {
		switch {
		case current == "":
			current = word
		case len(current)+1+len(word) <= width:
			current += " " + word
		default:
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" || len(lines) == 0 {
		lines = append(lines, current)
	}
	return lines
}
