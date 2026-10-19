package cli

import "github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"

// Some agents run hooks but have no Kemory plugin. For them connect writes
// the MCP entry for the tools and adds hooks that call `kemory hook`, so
// memory is loaded and saved without the model having to ask.

type memoryHook struct {
	install   func(hosts.Launch) (string, error)
	installed func() bool
	// approve is what the user must do before the agent runs the hooks.
	approve string
}

var memoryHooks = map[string]memoryHook{
	"codex": {
		install:   hosts.InstallCodexHooks,
		installed: hosts.CodexHooksInstalled,
		approve:   "approve them once with /hooks in Codex",
	},
}

// needsHooks reports whether host takes memory hooks that are not there yet.
func needsHooks(host string) bool {
	h, ok := memoryHooks[host]
	return ok && !h.installed()
}

// addHooks installs host's memory hooks and returns the line to report.
func addHooks(host string, launch hosts.Launch) (string, error) {
	h, ok := memoryHooks[host]
	if !ok {
		return "", nil
	}
	path, err := h.install(launch)
	if err != nil {
		return "", err
	}
	return "automatic memory hooks in " + path + "; " + h.approve, nil
}
