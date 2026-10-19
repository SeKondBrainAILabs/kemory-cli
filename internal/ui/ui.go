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

// Icon is a host's coloured tile, four columns wide.
func Icon(host string) string {
	c, ok := clients[host]
	if !ok {
		c = client{glyph: "•", bg: Purple, fg: white}
	}
	return lipgloss.NewStyle().Background(c.bg).Foreground(c.fg).Bold(true).Width(4).Align(lipgloss.Center).Render(c.glyph)
}

// Label is a host's icon, name and status for a list row.
func Label(host, status string) string {
	name := lipgloss.NewStyle().Bold(true).Width(16).Render(Name(host))
	return Icon(host) + " " + name + lipgloss.NewStyle().Foreground(Muted).Render(status)
}

var (
	check = lipgloss.NewStyle().Foreground(Mint).Bold(true).Render("✓")
	cross = lipgloss.NewStyle().Foreground(Orange).Bold(true).Render("✗")
)

// Result is a finished row: a mark, the host and what happened.
func Result(host string, ok bool, detail string) string {
	mark := check
	if !ok {
		mark = cross
	}
	return " " + mark + " " + Icon(host) + " " + lipgloss.NewStyle().Bold(true).Width(16).Render(Name(host)) +
		lipgloss.NewStyle().Foreground(Muted).Render(detail)
}

// Connected is a row for a host that already has Kemory, shown apart from
// the choices.
func Connected(host, status string) string {
	return " " + check + " " + Icon(host) + " " + lipgloss.NewStyle().Width(16).Render(Name(host)) +
		lipgloss.NewStyle().Foreground(Muted).Render(status)
}

// Heading is a small section heading.
func Heading(text string) string {
	return " " + lipgloss.NewStyle().Foreground(Muted).Bold(true).Render(text)
}

// Done is a top-level success line.
func Done(text string) string { return " " + check + " " + text }

// Summary is the closing box: a headline and the next steps.
func Summary(headline string, steps []string) string {
	body := lipgloss.NewStyle().Bold(true).Foreground(Purple).Render(headline)
	for _, s := range steps {
		body += "\n" + lipgloss.NewStyle().Foreground(Pink).Render("→ ") + s
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Purple).
		Padding(0, 2).
		MarginLeft(1).
		Render(body)
}

// Theme is the picker's look, in Kemory's colours.
func Theme() *huh.Theme {
	t := huh.ThemeCharm()
	t.Focused.Title = t.Focused.Title.Foreground(Purple).Bold(true)
	t.Focused.Description = t.Focused.Description.Foreground(Muted)
	t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(Pink).SetString("› ")
	t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(Mint).SetString("◉ ")
	t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(Muted).SetString("○ ")
	t.Focused.SelectedOption = lipgloss.NewStyle()
	t.Focused.UnselectedOption = lipgloss.NewStyle()
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(Purple)
	t.Blurred = t.Focused
	t.Group.Title = t.Focused.Title
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
			fmt.Fprintf(w, "\r %s %s", style.Render(frames[i%len(frames)]), title)
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
