package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

// quitTimeout is how long kemory waits for an app to quit, which includes
// the user answering the app's own "quit while sessions run?" dialog.
const quitTimeout = 60 * time.Second

// closeFirst explains why an app that rewrites its config while it runs was
// left alone, and what to do instead.
func closeFirst(host string) string {
	return fmt.Sprintf("The app is open and rewrites its config from memory, which would drop the change. "+
		"Quit it completely (⌘Q on macOS), then run `kemory mcp install --host %s`.", host)
}

// insideAppNote is shown instead of offering to quit an app this process runs
// in: quitting it would stop kemory halfway.
const insideAppNote = "This terminal runs inside Claude Desktop, so quitting the app would stop kemory too. " +
	"Run the command from Terminal or iTerm instead."

// withAppClosed applies change to host's config while its app is closed. An
// app that rewrites its config while it runs is quit first, with the user's
// consent (or yes), and started again afterwards. It returns a note for the
// user, and whether change ran.
func withAppClosed(host string, yes bool, change func() error) (bool, string, error) {
	if !hosts.RunningCheck(host) {
		return true, "", change()
	}
	if hosts.InsideApp(host) {
		return false, insideAppNote, nil
	}
	if !yes {
		if !stdinIsTerminal() {
			return false, closeFirst(host), nil
		}
		if !confirm("Claude Desktop is open and rewrites its config while it runs. Quit it, update the config and open it again?") {
			return false, closeFirst(host), nil
		}
	}
	render.Muted("Quitting Claude Desktop…")
	if err := hosts.QuitApp(host, quitTimeout); err != nil {
		if errors.Is(err, hosts.ErrNoAppControl) {
			return false, closeFirst(host), nil
		}
		return false, "could not quit Claude Desktop: " + err.Error() + ". " + closeFirst(host), nil
	}
	changeErr := change()
	note := "Claude Desktop was restarted"
	if err := hosts.OpenApp(host); err != nil {
		note = "open Claude Desktop again to load the change"
	}
	return changeErr == nil, note, changeErr
}
