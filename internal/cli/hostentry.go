package cli

import (
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/ui"
)

// restartStep is the note telling the user to restart a host after its config
// changed. Warp picks up its config on save, so it gets none.
func restartStep(host string) string {
	if host == "warp" {
		return ""
	}
	return "restart " + ui.AppName(host)
}

// entryWrite is what writing a host's kemory entry and memory hooks did.
type entryWrite struct {
	host string
	// done is false when the app was running and could not be closed; note
	// then says why. Otherwise note says what happened to the app.
	done     bool
	note     string
	hooks    string
	hooksErr error
}

// writeEntryWithHooks writes the kemory entry with the host's app closed, then
// adds the memory hooks where the host takes them. A hooks failure does not
// fail the write: it is kept in hooksErr.
func writeEntryWithHooks(host, path, server string, launch hosts.Launch, yes bool, resume []string) (entryWrite, error) {
	done, note, err := withAppClosed(host, yes, resume, func() error {
		return hosts.WriteEntry(host, path, server, launch)
	})
	if err != nil {
		return entryWrite{}, err
	}
	if !done {
		return entryWrite{host: host, note: note}, nil
	}
	w := entryWrite{host: host, done: true, note: note}
	w.hooks, w.hooksErr = addHooks(host, launch)
	return w, nil
}

// restart reports whether connect should tell the user to restart the host:
// not for Warp, and not where the hooks must be approved first, which connect
// asks for instead.
func (w entryWrite) restart() bool {
	return w.host != "warp" && (w.hooks == "" || memoryHooks[w.host].approve == "")
}

// hooksNote is the hooks part of a result line.
func (w entryWrite) hooksNote() string {
	if w.hooksErr != nil {
		return "memory hooks not added: " + w.hooksErr.Error()
	}
	return w.hooks
}
