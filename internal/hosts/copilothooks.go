package hosts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// GitHub Copilot reads user hooks from every file in ~/.copilot/hooks
// ($COPILOT_HOME/hooks): VS Code's agent mode and the Copilot CLI alike, each
// with its own payload shape, which `kemory hook` tells apart. Kemory owns
// kemory.json there, so connecting again rewrites the whole file. Neither
// asks approval for user hooks.

// copilotHookEvents are the Copilot events kemory hooks, and the event each
// capture is planned as. VS Code maps agentStop to Stop; it has no
// camelCase preCompact.
var copilotHookEvents = []struct{ event, hook, as string }{
	{"sessionStart", "session-start", ""},
	{"agentStop", "capture", "Stop"},
	{"preCompact", "capture", "PreCompact"},
	{"sessionEnd", "capture", "SessionEnd"},
}

// CopilotHome is Copilot's config directory.
func CopilotHome() string {
	if dir := os.Getenv("COPILOT_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(home(), ".copilot")
}

// CopilotHooksPath is the hooks file kemory owns.
func CopilotHooksPath() string {
	return filepath.Join(CopilotHome(), "hooks", "kemory.json")
}

type copilotHandler struct {
	Type       string `json:"type"`
	Command    string `json:"command"`
	TimeoutSec int    `json:"timeoutSec"`
}

type copilotHooksFile struct {
	Version int                         `json:"version"`
	Hooks   map[string][]copilotHandler `json:"hooks"`
}

func copilotHooksFor(launch Launch) copilotHooksFile {
	f := copilotHooksFile{Version: 1, Hooks: map[string][]copilotHandler{}}
	for _, e := range copilotHookEvents {
		command := hookCommand(launch, e.hook, "copilot")
		if e.as != "" {
			command += " --event " + e.as
		}
		f.Hooks[e.event] = []copilotHandler{{Type: "command", Command: command, TimeoutSec: 10}}
	}
	return f
}

// InstallCopilotHooks writes kemory's Copilot hooks file.
func InstallCopilotHooks(launch Launch) (string, error) {
	path := CopilotHooksPath()
	body, err := json.MarshalIndent(copilotHooksFor(launch), "", "  ")
	if err != nil {
		return path, err
	}
	return path, writeFile(path, append(body, '\n'))
}

// CopilotHooksInstalled reports whether kemory's Copilot hooks file has every
// hook.
func CopilotHooksInstalled() bool {
	data, err := os.ReadFile(CopilotHooksPath())
	if err != nil {
		return false
	}
	var f copilotHooksFile
	if json.Unmarshal(data, &f) != nil {
		return false
	}
	for _, e := range copilotHookEvents {
		handlers := f.Hooks[e.event]
		if len(handlers) == 0 || !isKemoryHook(handlers[0].Command, "copilot") || !strings.Contains(handlers[0].Command, " hook "+e.hook+" ") {
			return false
		}
	}
	return true
}
