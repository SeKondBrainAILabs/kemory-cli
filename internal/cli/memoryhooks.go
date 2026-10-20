package cli

import "github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"

// Some agents run hooks but have no Kemory plugin. For them connect writes
// the MCP entry for the tools and adds hooks that call `kemory hook`, so
// memory is loaded and saved without the model having to ask.

type memoryHook struct {
	install   func(hosts.Launch) (string, error)
	installed func() bool
	path      func() string
	// approve is what the user must do before the agent runs the hooks, if
	// anything.
	approve string
	// found and remove find and take out any kemory hook, for a host that
	// can reach Kemory another way.
	found  func() bool
	remove func() error
}

var memoryHooks = map[string]memoryHook{
	"codex": {
		install:   hosts.InstallCodexHooks,
		installed: hosts.CodexHooksInstalled,
		path:      hosts.CodexHooksPath,
		approve:   "approve them once with /hooks in Codex",
	},
	"cursor": {
		install:   hosts.InstallCursorHooks,
		installed: hosts.CursorHooksInstalled,
		path:      hosts.CursorHooksPath,
		found:     hosts.CursorHooksFound,
		remove:    hosts.RemoveCursorHooks,
	},
	"vscode": {
		install:   hosts.InstallCopilotHooks,
		installed: hosts.CopilotHooksInstalled,
		path:      hosts.CopilotHooksPath,
	},
	// VS Code and the Copilot CLI share one hooks file.
	"copilot-cli": {
		install:   hosts.InstallCopilotHooks,
		installed: hosts.CopilotHooksInstalled,
		path:      hosts.CopilotHooksPath,
	},
	"droid": {
		install:   hosts.InstallDroidHooks,
		installed: hosts.DroidHooksInstalled,
		path:      hosts.DroidHooksPath,
	},
	"gemini-cli": {
		install:   hosts.InstallGeminiHooks,
		installed: hosts.GeminiHooksInstalled,
		path:      hosts.GeminiSettingsPath,
	},
	"opencode": {
		install:   hosts.InstallOpenCodePlugin,
		installed: hosts.OpenCodePluginInstalled,
		path:      hosts.OpenCodePluginPath,
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
	line := "automatic memory hooks in " + path
	if h.approve != "" {
		line += "; " + h.approve
	}
	return line, nil
}

// leftoverHooks reports whether host has kemory hooks that a covered host
// does not need.
func leftoverHooks(host string) bool {
	h, ok := memoryHooks[host]
	return ok && h.found != nil && h.found()
}
