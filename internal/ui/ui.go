// Package ui draws the guided `kemory connect` screen: the banner, a coloured
// icon per AI client, the picker theme, a spinner and the closing summary.
// It is used only in an interactive terminal; everywhere else the CLI prints
// plain lines.
package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Kemory's brand colours (SeKondBrain design tokens).
var (
	Blue   = lipgloss.Color("#1A8AF6")
	Purple = lipgloss.Color("#8B78F5")
	Lilac  = lipgloss.Color("#A88AF8")
	Pink   = lipgloss.Color("#E24AF2")
	Mint   = lipgloss.Color("#3DD680")
	Orange = lipgloss.Color("#F28B68")
	Yellow = lipgloss.Color("#E6B800")
	Muted  = lipgloss.AdaptiveColor{Light: "245", Dark: "243"}
)

// MinWidth is the narrowest terminal the header with the mark is drawn in;
// below it the one-line title is used.
const MinWidth = 60

// Header is the SeKondBrain mark with the kemory name, tagline and maker
// beside it.
func Header() string {
	text := map[int]string{
		2: wordmark("kemory"),
		3: lipgloss.NewStyle().Foreground(Lilac).Render("persistent memory for your AI agents"),
		4: lipgloss.NewStyle().Foreground(Muted).Render("by SeKondBrain"),
	}
	var b strings.Builder
	for i, line := range markLines() {
		b.WriteString(" " + line)
		if t := text[i]; t != "" {
			b.WriteString("   " + t)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// wordmark is word in bold, in a blue-to-purple-to-pink gradient.
func wordmark(word string) string {
	stops := []rgb{hex(Blue), hex(Purple), hex(Pink)}
	runes := []rune(word)
	var b strings.Builder
	for i, r := range runes {
		c := gradient(stops, float64(i)/float64(len(runes)-1))
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(c.String())).Render(string(r)))
	}
	return b.String()
}

// Title is the one-line heading used where the banner does not fit.
func Title() string {
	return lipgloss.NewStyle().Foreground(Purple).Bold(true).Render("◆ kemory") +
		lipgloss.NewStyle().Foreground(Lilac).Render(" · persistent memory for your AI agents")
}

type client struct {
	name, glyph string
	bg, fg      lipgloss.TerminalColor
}

// mono is the tile for products whose mark is black: light on a dark
// terminal and dark on a light one, so it never disappears into the
// background.
var (
	monoBg = lipgloss.AdaptiveColor{Light: "#1E1E1E", Dark: "#E6E6E6"}
	monoFg = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#111111"}
	white  = lipgloss.Color("#FFFFFF")
)

// clients is how each host is shown: its product name and a tile in the
// product's colour holding its mark where a character draws it (Claude's
// asterisk, Gemini's sparkle) and a two-letter monogram everywhere else.
// Terminals cannot draw the real logos reliably.
var clients = map[string]client{
	"claude-code":    {"Claude Code", "✻", lipgloss.Color("#D97757"), white},
	"claude-desktop": {"Claude Desktop", "✻", lipgloss.Color("#C15F3C"), white},
	"codex":          {"Codex", "Cx", monoBg, monoFg},
	"cursor":         {"Cursor", "Cu", monoBg, monoFg},
	"vscode":         {"VS Code", "VS", lipgloss.Color("#007ACC"), white},
	"gemini-cli":     {"Gemini CLI", "✦", lipgloss.Color("#4285F4"), white},
	"antigravity":    {"Antigravity", "Ag", lipgloss.Color("#A142F4"), white},
	"windsurf":       {"Windsurf", "Ws", lipgloss.Color("#0B9A8D"), white},
	"cline":          {"Cline", "Cl", monoBg, monoFg},
	"kiro":           {"Kiro", "Ki", lipgloss.Color("#9046FF"), white},
	"grok":           {"Grok", "Gr", monoBg, monoFg},
	"continue":       {"Continue", "Co", lipgloss.Color("#E8A33D"), lipgloss.Color("#000000")},
	"warp":           {"Warp", "Wa", lipgloss.Color("#01A4FF"), white},
	"opencode":       {"OpenCode", "OC", monoBg, monoFg},
}

// Name is the product name of a host.
func Name(host string) string {
	if c, ok := clients[host]; ok {
		return c.name
	}
	return host
}

// tile is a host's monogram tile, two columns wide, for terminals that
// cannot draw its logo.
func tile(host string) string {
	c, ok := clients[host]
	if !ok {
		c = client{glyph: "•", bg: Purple, fg: white}
	}
	return lipgloss.NewStyle().Background(c.bg).Foreground(c.fg).Bold(true).Width(2).Render(c.glyph)
}

// The rail runs down the left of the guided screen, as in clack's prompts:
// ┌ opens it, │ carries each line, ◇ marks an answered question and └
// closes it.
var railStyle = lipgloss.NewStyle().Foreground(Purple)

// Start opens the rail with text.
func Start(text string) string { return railStyle.Render("┌") + "  " + text }

// Rail is a line on the rail.
func Rail(text string) string {
	if text == "" {
		return railStyle.Render("│")
	}
	return railStyle.Render("│") + "  " + text
}

// Answered is a question and its answer, once given.
func Answered(question, answer string) string {
	return railStyle.Render("◇") + "  " + question + "\n" + Rail(dim.Render(answer))
}

// End closes the rail with text.
func End(text string) string { return railStyle.Render("└") + "  " + text }

var (
	dim   = lipgloss.NewStyle().Foreground(Muted)
	bold  = lipgloss.NewStyle().Bold(true)
	check = lipgloss.NewStyle().Foreground(Mint).Bold(true).Render("✓")
	cross = lipgloss.NewStyle().Foreground(Orange).Bold(true).Render("✗")
)

// Dim is secondary text.
func Dim(text string) string { return dim.Render(text) }

// Bold is emphasised text.
func Bold(text string) string { return bold.Render(text) }

// Detected is a row of the detected-clients list: its state, logo, name
// and what Kemory gives it.
func Detected(host string, ready bool, status string) string {
	tag := lipgloss.NewStyle().Foreground(Muted).Width(12).Render("[connected]")
	if ready {
		tag = lipgloss.NewStyle().Foreground(Mint).Width(12).Render("[ready]")
	}
	return tag + Logo(host) + " " + bold.Width(16).Render(Name(host)) + dim.Render(status)
}

// Option is a host as a choice in a prompt, where logos cannot be drawn.
func Option(host, status string) string {
	return lipgloss.NewStyle().Width(16).Render(Name(host)) + dim.Render(status)
}

// Result is a finished row: a mark, the host and what happened.
func Result(host string, ok bool, detail string) string {
	mark := check
	if !ok {
		mark = cross
	}
	return mark + " " + Logo(host) + " " + bold.Width(16).Render(Name(host)) + dim.Render(detail)
}

// Done is a success line.
func Done(text string) string { return check + " " + text }

// Step is a next step for the user.
func Step(text string) string { return lipgloss.NewStyle().Foreground(Pink).Render("→") + " " + text }

// Theme is the prompts' look: on the rail, in Kemory's colours.
func Theme() *huh.Theme {
	t := huh.ThemeBase()
	t.Focused.Base = lipgloss.NewStyle().
		Border(lipgloss.Border{Left: "│"}, false, false, false, true).
		BorderForeground(Purple).
		PaddingLeft(2)
	t.Focused.Title = lipgloss.NewStyle().Bold(true)
	t.Focused.Description = dim
	t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(Pink).SetString("● ")
	t.Focused.Option = dim
	t.Focused.SelectedOption = lipgloss.NewStyle()
	t.Focused.MultiSelectSelector = lipgloss.NewStyle().Foreground(Pink).SetString("› ")
	t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(Mint).SetString("◉ ")
	t.Focused.UnselectedPrefix = dim.SetString("○ ")
	t.Focused.UnselectedOption = dim
	t.Blurred = t.Focused
	t.Help.ShortKey = dim
	t.Help.ShortDesc = dim
	t.Help.ShortSeparator = dim
	return t
}

// Spin shows a spinner with title on w while work runs, then clears it.
func Spin(w io.Writer, title string, work func()) {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	style := lipgloss.NewStyle().Foreground(Pink)
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		for i := 0; ; i++ {
			fmt.Fprintf(w, "\r%s", Rail(style.Render(frames[i%len(frames)])+" "+title))
			select {
			case <-done:
				fmt.Fprint(w, "\r\033[2K")
				return
			case <-tick.C:
			}
		}
	}()
	work()
	close(done)
	wg.Wait()
}
