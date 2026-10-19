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

var banner = []string{
	"██╗  ██╗███████╗███╗   ███╗ ██████╗ ██████╗ ██╗   ██╗",
	"██║ ██╔╝██╔════╝████╗ ████║██╔═══██╗██╔══██╗╚██╗ ██╔╝",
	"█████╔╝ █████╗  ██╔████╔██║██║   ██║██████╔╝ ╚████╔╝ ",
	"██╔═██╗ ██╔══╝  ██║╚██╔╝██║██║   ██║██╔══██╗  ╚██╔╝  ",
	"██║  ██╗███████╗██║ ╚═╝ ██║╚██████╔╝██║  ██║   ██║   ",
	"╚═╝  ╚═╝╚══════╝╚═╝     ╚═╝ ╚═════╝ ╚═╝  ╚═╝   ╚═╝   ",
}

// bannerWidth is the banner's width in columns.
const bannerWidth = 53

// MinWidth is the narrowest terminal the full banner is drawn in.
const MinWidth = bannerWidth + 4

// Banner is the KEMORY wordmark in a blue-to-purple-to-pink gradient, with
// the tagline under it.
func Banner() string {
	stops := []rgb{hex(Blue), hex(Purple), hex(Pink)}
	var b strings.Builder
	for _, line := range banner {
		b.WriteString(" ")
		runes := []rune(line)
		for i, r := range runes {
			c := gradient(stops, float64(i)/float64(len(runes)-1))
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(c.String())).Render(string(r)))
		}
		b.WriteString("\n")
	}
	b.WriteString(" " + lipgloss.NewStyle().Foreground(Lilac).Render("persistent memory for your AI agents") + "\n")
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

// clients is how each host is shown: its product name and an icon tile in
// the product's colour.
var clients = map[string]client{
	"claude-code":    {"Claude Code", "✻", lipgloss.Color("#D97757"), white},
	"claude-desktop": {"Claude Desktop", "✻", lipgloss.Color("#C15F3C"), white},
	"codex":          {"Codex", "◎", monoBg, monoFg},
	"cursor":         {"Cursor", "◆", monoBg, monoFg},
	"vscode":         {"VS Code", "◈", lipgloss.Color("#007ACC"), white},
	"gemini-cli":     {"Gemini CLI", "✦", lipgloss.Color("#4285F4"), white},
	"antigravity":    {"Antigravity", "▲", lipgloss.Color("#A142F4"), white},
	"windsurf":       {"Windsurf", "≋", lipgloss.Color("#0B9A8D"), white},
	"cline":          {"Cline", "◉", monoBg, monoFg},
	"kiro":           {"Kiro", "◇", lipgloss.Color("#9046FF"), white},
	"grok":           {"Grok", "✕", monoBg, monoFg},
	"continue":       {"Continue", "▶", lipgloss.Color("#E8A33D"), lipgloss.Color("#000000")},
	"warp":           {"Warp", "»", lipgloss.Color("#01A4FF"), white},
	"opencode":       {"OpenCode", "▣", monoBg, monoFg},
}

// Name is the product name of a host.
func Name(host string) string {
	if c, ok := clients[host]; ok {
		return c.name
	}
	return host
}

// Icon is a host's coloured tile, three columns wide.
func Icon(host string) string {
	c, ok := clients[host]
	if !ok {
		return lipgloss.NewStyle().Background(Purple).Foreground(white).Render(" • ")
	}
	return lipgloss.NewStyle().Background(c.bg).Foreground(c.fg).Bold(true).Render(" " + c.glyph + " ")
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
