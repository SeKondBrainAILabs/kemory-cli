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
func Echo(text string) { fmt.Fprintln(stdout(), text) }

// Accent prints one highlighted line to stderr.
func Accent(text string) { fmt.Fprintln(Err, paint(Err, styleAccent, text)) }

// Muted prints secondary context. Dropped by --quiet.
func Muted(text string) {
	if quiet {
		return
	}
	w := stdout()
	fmt.Fprintln(w, paint(w, styleMuted, text))
}

// Success confirms something that just happened. Dropped by --quiet.
func Success(text string) {
	if quiet {
		return
	}
	w := stdout()
	fmt.Fprintln(w, paint(w, styleSuccess, text))
}

// Warn is advisory, not a failure. Always stderr.
func Warn(text string) { fmt.Fprintln(Err, paint(Err, styleWarn, text)) }

// Error prints a failure message to stderr in the "Error: ..." shape.
func Error(text string) { fmt.Fprintln(Err, paint(Err, styleError, "Error: ")+text) }

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

// Heading prints a bold title line.
func Heading(text string) {
	w := stdout()
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
