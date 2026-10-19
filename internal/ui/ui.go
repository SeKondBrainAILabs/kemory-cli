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

// MinWidth is the narrowest terminal the three-line header is drawn in;
// below it the one-line title is used.
const MinWidth = 60

// circle is the SeKondBrain mark, a ring around a disc, as a character every
// terminal draws.
var circle = lipgloss.NewStyle().Foreground(Purple).Bold(true).Render("◉")

// Header is the SeKondBrain mark in line with kemory's name, and the tagline
// and maker under it.
func Header() string {
	return " " + circle + " " + wordmark("kemory") + "\n" +
		"   " + lipgloss.NewStyle().Foreground(Lilac).Render("persistent memory for your AI agents") + "\n" +
		"   " + dim.Render("by SeKondBrain") + "\n"
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

// Title is the one-line heading used where the header does not fit.
func Title() string {
	return circle + " " + wordmark("kemory") + lipgloss.NewStyle().Foreground(Lilac).Render(" · persistent memory for your AI agents")
}

// names are the product names of the hosts.
var names = map[string]string{
	"claude-code":    "Claude Code",
	"claude-desktop": "Claude Desktop",
	"codex":          "Codex",
	"cursor":         "Cursor",
	"vscode":         "VS Code",
	"gemini-cli":     "Gemini CLI",
	"antigravity":    "Antigravity",
	"windsurf":       "Windsurf",
	"cline":          "Cline",
	"kiro":           "Kiro",
	"grok":           "Grok",
	"continue":       "Continue",
	"warp":           "Warp",
	"opencode":       "OpenCode",
}

// Name is the product name of a host.
func Name(host string) string {
	if n, ok := names[host]; ok {
		return n
	}
	return host
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

// Mark is the SeKondBrain mark, ◉ in Kemory's colour.
func Mark() string { return circle }

// Section is a section title.
func Section(title string) string {
	return lipgloss.NewStyle().Foreground(Purple).Bold(true).Render(title)
}

// Dim is secondary text.
func Dim(text string) string { return dim.Render(text) }

// Bold is emphasised text.
func Bold(text string) string { return bold.Render(text) }

// Detected is a row of the detected-clients list: a green dot when the
// client already has Kemory, a grey ring when it does not yet, then its name
// and what Kemory gives it or would add.
func Detected(host string, ready bool, status string) string {
	dot := lipgloss.NewStyle().Foreground(Mint).Render("●")
	if ready {
		dot = dim.Render("○")
	}
	return dot + " " + bold.Width(16).Render(Name(host)) + dim.Render(status)
}

// Option is a host as a choice in a prompt.
func Option(host, status string) string {
	return lipgloss.NewStyle().Width(16).Render(Name(host)) + dim.Render(status)
}

// Result is a finished row: a mark, the host and what happened.
func Result(host string, ok bool, detail string) string {
	mark := check
	if !ok {
		mark = cross
	}
	return mark + " " + bold.Width(16).Render(Name(host)) + dim.Render(detail)
}

// Failed is a failure line.
func Failed(text string) string { return cross + " " + text }

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
	t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(Pink).SetString("› ")
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
