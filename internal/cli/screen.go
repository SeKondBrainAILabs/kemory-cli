package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"golang.org/x/term"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/ui"
)

// screen is the guided `kemory connect`: banner, picker, progress and a
// closing summary. It exists only in an interactive, colour terminal; every
// method is a no-op on a nil screen, so callers need not check.
type screen struct {
	width     int
	connected int
	next      []string
	restart   []string
}

// terminalWidth is the width of an interactive terminal on both stdin and
// stdout, or 0 when either is not one. A variable so tests can pretend.
var terminalWidth = func() int {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return 0
	}
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80
	}
	return w
}

// newScreen returns the guided screen, or nil when plain output is wanted:
// no terminal, JSON output, --quiet, no colour, or KEMORY_ACCESSIBLE=1.
func newScreen(g *globals) *screen {
	if render.JSONMode() || g.quiet || g.noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("KEMORY_ACCESSIBLE") == "1" {
		return nil
	}
	w := terminalWidth()
	if w == 0 {
		return nil
	}
	return &screen{width: w}
}

func (s *screen) banner() {
	if s == nil {
		return
	}
	render.Echo("")
	if s.width >= ui.MinWidth {
		render.Echo(ui.Banner())
	} else {
		render.Echo(" " + ui.Title() + "\n")
	}
}

// spin runs work under a spinner naming host. An app that may ask to be quit
// gets no spinner, so its question is not drawn over.
func (s *screen) spin(host string, work func()) {
	if s == nil || hosts.RunningCheck(host) {
		work()
		return
	}
	ui.Spin(render.Err, "Connecting "+ui.Name(host)+"…", work)
}

// results prints the finished rows and remembers the next steps.
func (s *screen) results(lines []hostResult) {
	render.Echo("")
	for _, r := range lines {
		render.Echo(ui.Result(r.host, r.ok, orDefault(r.short, r.detail)))
		if r.ok && !strings.HasPrefix(r.short, "already") {
			s.connected++
		}
		if r.next != "" && !contains(s.next, r.next) {
			s.next = append(s.next, r.next)
		}
		if r.restart {
			s.restart = append(s.restart, ui.Name(r.host))
		}
	}
	render.Echo("")
}

// summary closes the screen with who is signed in and what is left to do.
func (s *screen) summary(who string) {
	headline := "Kemory is connected for " + who
	if s.connected > 0 {
		headline = fmt.Sprintf("Kemory is connected to %d client%s for %s", s.connected, plural(s.connected), who)
	}
	steps := append([]string{}, s.next...)
	if len(s.restart) > 0 {
		steps = append(steps, "Restart "+listJoin(s.restart)+" to load Kemory")
	}
	steps = append(steps, "Check everything at any time with `kemory doctor`")
	render.Echo(ui.Summary(headline, steps))
	render.Echo("")
}

// listJoin joins names as "A, B and C".
func listJoin(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// pickFancy asks which hosts to connect with an arrow-key multi-select.
func (s *screen) pickFancy(rows []pickRow, missing []string) ([]string, error) {
	var options []huh.Option[string]
	var ready int
	for _, r := range rows {
		options = append(options, huh.NewOption(ui.Label(r.host, r.short), r.host).Selected(r.state == "ready"))
		if r.state == "ready" {
			ready++
		}
	}
	if ready == 0 {
		render.Echo(ui.Done("Every AI client found here is already connected."))
		return nil, nil
	}
	description := ""
	if len(missing) > 0 {
		names := make([]string, len(missing))
		for i, m := range missing {
			names[i] = ui.Name(m)
		}
		description = "Not found here: " + strings.Join(names, ", ")
	}
	var picked []string
	field := huh.NewMultiSelect[string]().
		Title("Which AI clients should Kemory connect?").
		Description(description).
		Options(options...).
		Value(&picked)
	keys := huh.NewDefaultKeyMap()
	keys.MultiSelect.Toggle = key.NewBinding(key.WithKeys(" ", "x"), key.WithHelp("space", "toggle"))
	keys.MultiSelect.Filter = key.NewBinding(key.WithDisabled())
	err := huh.NewForm(huh.NewGroup(field)).WithTheme(ui.Theme()).WithKeyMap(keys).WithOutput(render.Err).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return nil, exits.New("Cancelled; nothing was changed.", exits.Failure)
	}
	if err != nil {
		return nil, err
	}
	if len(picked) == 0 {
		render.Muted(" Nothing selected; nothing was changed.")
	}
	return picked, nil
}
