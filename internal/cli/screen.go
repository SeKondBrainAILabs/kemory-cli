package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"golang.org/x/term"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/ui"
)

// screen is the guided `kemory connect`: banner, picker, progress and a
// closing summary. It exists only in an interactive, colour terminal; every
// method is a no-op on a nil screen, so callers need not check.
type screen struct {
	width int
	// railOpen is set once a line has opened the rail with ┌.
	railOpen  bool
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
		render.Echo(ui.Header())
	} else {
		render.Echo(" " + ui.Title() + "\n")
	}
}

// signedIn says which account and org kemory connects for, and the server
// when it is not the default one.
func (s *screen) signedIn(g *globals, creds *config.Credentials) {
	if s == nil {
		return
	}
	who := "Signed in"
	if creds.Email != "" {
		who += " as " + creds.Email
	}
	if org := orgNameFor(creds); org != "" {
		who += ui.Dim(" · " + org)
	}
	if g.env != config.DefaultEnv {
		if u, err := url.Parse(creds.KemoryURL); err == nil && u.Host != "" {
			who += ui.Dim(" · " + u.Host)
		}
	}
	if s.railOpen {
		render.Echo(ui.Rail(ui.Done(who)))
		return
	}
	s.railOpen = true
	render.Echo(ui.Start(who))
}

// orgNameFor is the name of the org creds act in, or "" when it cannot be
// told.
func orgNameFor(creds *config.Credentials) string {
	resp, err := api.Get(creds, "/api/v1/me")
	if err != nil || resp.Status != 200 {
		return ""
	}
	var m struct {
		OrgID   string `json:"org_id"`
		OrgName string `json:"org_name"`
	}
	if resp.JSON(&m) != nil {
		return ""
	}
	if m.OrgName == "" || m.OrgName == m.OrgID {
		return resolveOrgName(creds, m.OrgID)
	}
	return m.OrgName
}

// loginUI draws the device sign-in on the rail: where to go, the code, and
// a spinner while the browser approval is pending.
func (s *screen) loginUI() auth.LoginUI {
	return auth.LoginUI{
		ShowCode: func(uri, code string) {
			s.railOpen = true
			render.Echo(ui.Start("Sign in to Kemory"))
			render.Echo(ui.Rail(ui.Dim("Open  ") + uri))
			render.Echo(ui.Rail(ui.Dim("Code  ") + ui.Bold(code)))
		},
		Wait: func(poll func()) {
			ui.Spin(render.Err, "Waiting for you to approve in the browser…", poll)
		},
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
	render.Echo(ui.Rail(""))
	for _, r := range lines {
		render.Echo(ui.Rail(ui.Result(r.host, r.ok, orDefault(r.short, r.detail))))
		if r.ok && !strings.HasPrefix(r.short, "already") {
			s.connected++
		}
		if r.next != "" && !contains(s.next, r.next) {
			s.next = append(s.next, r.next)
		}
		if r.restart && !contains(s.restart, ui.AppName(r.host)) {
			s.restart = append(s.restart, ui.AppName(r.host))
		}
	}
}

// summary closes the rail with the next steps and who is connected.
func (s *screen) summary(who string) {
	steps := append([]string{}, s.next...)
	if len(s.restart) > 0 {
		steps = append(steps, "Restart "+listJoin(s.restart)+" to load Kemory")
	}
	steps = append(steps, "Check everything at any time with "+ui.Bold("kemory doctor"))
	render.Echo(ui.Rail(""))
	render.Echo(ui.Rail(ui.Bold("Next")))
	for _, step := range steps {
		render.Echo(ui.Rail(ui.Step(step)))
	}
	render.Echo(ui.Rail(""))
	headline := "Kemory is connected for " + who
	if s.connected > 0 {
		headline = fmt.Sprintf("Kemory is connected to %d client%s for %s", s.connected, plural(s.connected), who)
	}
	render.Echo(ui.End(ui.Done(headline)))
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

// The answers to the first question.
const (
	connectAll = "all"
	chooseSome = "choose"
	cancelPick = "cancel"
)

const pickQuestion = "Which AI clients should Kemory connect?"

// pickFancy lists every supported client with its Kemory state, connected
// first, then the ones it can connect, then the ones not installed here, and
// asks one question: connect every ready client, choose some, or stop.
func (s *screen) pickFancy(rows []pickRow, missing []string) ([]string, error) {
	var ready []pickRow
	render.Echo(ui.Rail(""))
	for _, r := range rows {
		if r.mark != ui.Ready {
			render.Echo(ui.Rail(ui.Detected(r.host, r.mark, r.short)))
		}
	}
	for _, r := range rows {
		if r.mark == ui.Ready {
			render.Echo(ui.Rail(ui.Detected(r.host, ui.Ready, r.short)))
		}
		if r.state == "ready" {
			ready = append(ready, r)
		}
	}
	// The clients not installed here share one line, so the list is what
	// this machine has.
	if len(missing) > 0 {
		names := make([]string, len(missing))
		for i, h := range missing {
			names[i] = ui.ShortName(h)
		}
		for i, l := range wrap("Not installed: "+strings.Join(names, ", "), max(20, s.width-5)) {
			prefix := "○ "
			if i > 0 {
				prefix = "  "
			}
			render.Echo(ui.Rail(ui.Dim(prefix + l)))
		}
	}
	render.Echo(ui.Rail(ui.Dim(levelLegend)))
	render.Echo(ui.Rail(""))
	if len(ready) == 0 {
		render.Echo(ui.End(ui.Done("Every AI client here already has Kemory.")))
		render.Echo("")
		return nil, nil
	}

	all := "Connect " + ui.Name(ready[0].host)
	if len(ready) > 1 {
		all = fmt.Sprintf("Connect all %d ready clients", len(ready))
	}
	choice := connectAll
	err := s.ask(huh.NewSelect[string]().
		Title(pickQuestion).
		Description("↑↓ to move · enter to confirm").
		Options(huh.NewOption(all, connectAll), huh.NewOption("Choose clients…", chooseSome), huh.NewOption("Cancel", cancelPick)).
		Value(&choice), 3)
	if err != nil {
		return nil, err
	}
	var picked []string
	switch choice {
	case cancelPick:
		return nil, s.cancelled()
	case connectAll:
		for _, r := range ready {
			picked = append(picked, r.host)
		}
		render.Echo(ui.Answered(pickQuestion, all))
	case chooseSome:
		var options []huh.Option[string]
		for _, r := range ready {
			options = append(options, huh.NewOption(ui.Option(r.host, r.short), r.host).Selected(true))
		}
		field := huh.NewMultiSelect[string]().Title(pickQuestion).Description("space to toggle · enter to confirm").Options(options...).Value(&picked)
		if err := s.ask(field, len(options)); err != nil {
			return nil, err
		}
		if len(picked) == 0 {
			return nil, s.cancelled()
		}
		names := make([]string, len(picked))
		for i, h := range picked {
			names[i] = ui.Name(h)
		}
		render.Echo(ui.Answered(pickQuestion, listJoin(names)))
	}
	return picked, nil
}

// ask runs one prompt on the rail. Its height is set from its options, not
// taken from the terminal: some terminals (Warp) report a short height at
// first, and huh would keep the list that short, scrolling clients out of
// sight.
func (s *screen) ask(field huh.Field, options int) error {
	keys := huh.NewDefaultKeyMap()
	keys.MultiSelect.Toggle = key.NewBinding(key.WithKeys(" ", "x"), key.WithHelp("space", "toggle"))
	keys.MultiSelect.Filter = key.NewBinding(key.WithDisabled())
	keys.Select.Filter = key.NewBinding(key.WithDisabled())
	err := huh.NewForm(huh.NewGroup(field)).
		WithTheme(ui.Theme()).WithKeyMap(keys).WithOutput(render.Err).WithShowHelp(false).
		WithHeight(2 + options). // title, hint and options
		Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return s.cancelled()
	}
	return err
}

func (s *screen) cancelled() error {
	render.Echo(ui.End(ui.Dim("Cancelled; nothing was changed.")))
	render.Echo("")
	return exits.New("", exits.Failure)
}

// loading runs work under a spinner with title where one may be drawn.
func loading(title string, work func()) {
	if !render.Animated() {
		work()
		return
	}
	ui.Loading(render.Err, title, work)
}
