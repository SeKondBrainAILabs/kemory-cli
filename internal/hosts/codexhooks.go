package hosts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/jsonedit"
)

// Codex and Gemini CLI keep hooks in a JSON file's "hooks" object: for each
// event, a list of groups, each holding command handlers. Kemory adds one
// group per event that calls `kemory hook`, and leaves every other hook
// alone.
//
// Codex reads ~/.codex/hooks.json ($CODEX_HOME/hooks.json) and only runs a
// new or changed hook once the user trusts it with /hooks, which no file
// kemory could write can do for them. Gemini CLI reads the hooks in
// ~/.gemini/settings.json, next to its MCP servers, and asks nothing.

// hookEvent is an agent event, the kemory hook it runs and the handler's
// timeout in seconds.
type hookEvent struct {
	event, hook string
	timeout     int
}

// groupedHooks is an agent whose hooks file holds groups of handlers.
type groupedHooks struct {
	agent  string
	path   func() string
	events []hookEvent
	// timeoutScale turns seconds into the unit the agent counts in.
	timeoutScale int
	// named handlers carry a name, which the agent shows in its hook list.
	named bool
}

// Codex caps SessionEnd at 3 seconds.
var codexHooks = groupedHooks{
	agent: "codex", path: CodexHooksPath, timeoutScale: 1,
	events: []hookEvent{
		{"SessionStart", "session-start", 10},
		{"Stop", "capture", 10},
		{"PreCompact", "capture", 10},
		{"SessionEnd", "capture", 3},
	},
}

// Gemini CLI counts timeouts in milliseconds. AfterAgent fires after every
// turn.
var geminiHooks = groupedHooks{
	agent: "gemini-cli", path: GeminiSettingsPath, timeoutScale: 1000, named: true,
	events: []hookEvent{
		{"SessionStart", "session-start", 10},
		{"AfterAgent", "capture", 10},
		{"PreCompress", "capture", 10},
		{"SessionEnd", "capture", 10},
	},
}

// codexHome is Codex's home directory, $CODEX_HOME or ~/.codex.
func codexHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(home(), ".codex")
}

// CodexHooksPath is Codex's user-level hooks file.
func CodexHooksPath() string {
	return filepath.Join(codexHome(), "hooks.json")
}

// GeminiSettingsPath is Gemini CLI's user settings file, which holds its hooks
// as well as its MCP servers.
func GeminiSettingsPath() string {
	return filepath.Join(home(), ".gemini", "settings.json")
}

// shellQuote quotes an argument for the shell an agent runs hook commands in.
func shellQuote(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\n'\"\\$`;&|<>()*?[]{}!#~") {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// hookCommand is the command agent's hook runs for hook (session-start or
// capture), started the way launch starts kemory.
func hookCommand(launch Launch, hook, agent string) string {
	// launch.Args ends in "mcp serve"; keep what precedes it (the npx/uvx
	// package and --env) and run the hook instead.
	args := launch.Args
	if n := len(args); n >= 2 && args[n-2] == "mcp" && args[n-1] == "serve" {
		args = args[:n-2]
	}
	parts := []string{shellQuote(launch.Command)}
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	parts = append(parts, "hook", hook, "--agent", agent)
	return strings.Join(parts, " ")
}

// isKemoryHook reports whether a handler command is one kemory wrote for
// agent.
func isKemoryHook(command, agent string) bool {
	return strings.Contains(command, "kemory") &&
		(strings.Contains(command, " hook session-start --agent "+agent) || strings.Contains(command, " hook capture --agent "+agent))
}

type groupHandler struct {
	Name          string `json:"name,omitempty"`
	Type          string `json:"type"`
	Command       string `json:"command"`
	Timeout       int    `json:"timeout,omitempty"`
	StatusMessage string `json:"statusMessage,omitempty"`
}

type hookGroup struct {
	Matcher string         `json:"matcher,omitempty"`
	Hooks   []groupHandler `json:"hooks"`
}

// readHooks reads an agent's hooks file and its "hooks" object, both empty
// when the file does not exist.
func readHooks(path string) (*jsonedit.Object, *jsonedit.Object, error) {
	cfg := jsonedit.NewObject()
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if cfg, err = jsonedit.Parse(data); err != nil {
			return nil, nil, invalid(path, "is not valid JSON")
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, nil, err
	}
	events, err := cfg.Object("hooks")
	if err != nil {
		return nil, nil, invalid(path, "has a hooks value that is not a JSON object")
	}
	return cfg, events, nil
}

// withoutKemory returns an event's groups minus the handlers kemory wrote for
// agent, and whether any were found. A group left empty by that is dropped.
func withoutKemory(raw json.RawMessage, agent string) ([]json.RawMessage, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, false, err
	}
	var kept []json.RawMessage
	found := false
	for _, g := range groups {
		var group struct {
			Hooks []json.RawMessage `json:"hooks"`
		}
		if json.Unmarshal(g, &group) != nil {
			kept = append(kept, g)
			continue
		}
		var handlers []json.RawMessage
		for _, h := range group.Hooks {
			var handler struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(h, &handler) == nil && isKemoryHook(handler.Command, agent) {
				found = true
				continue
			}
			handlers = append(handlers, h)
		}
		switch {
		case len(handlers) == len(group.Hooks):
			kept = append(kept, g)
		case len(handlers) > 0:
			obj, err := jsonedit.Parse(g)
			if err != nil {
				return nil, false, err
			}
			if err := obj.SetValue("hooks", handlers); err != nil {
				return nil, false, err
			}
			out, err := obj.Marshal("")
			if err != nil {
				return nil, false, err
			}
			kept = append(kept, out)
		}
	}
	return kept, found, nil
}

// install adds kemory's hooks to the agent's hooks file, replacing any it
// added before, and leaves every other hook as it was.
func (g groupedHooks) install(launch Launch) (string, error) {
	path := g.path()
	cfg, events, err := readHooks(path)
	if err != nil {
		return path, err
	}
	for _, e := range g.events {
		raw, _ := events.Get(e.event)
		groups, _, err := withoutKemory(raw, g.agent)
		if err != nil {
			return path, invalid(path, fmt.Sprintf("has a %s value that is not a list of hook groups", e.event))
		}
		handler := groupHandler{Type: "command", Command: hookCommand(launch, e.hook, g.agent), Timeout: e.timeout * g.timeoutScale}
		if g.named {
			handler.Name = "kemory-" + e.hook
		}
		if g.agent == "codex" && e.hook == "session-start" {
			handler.StatusMessage = "Loading Kemory memory…"
		}
		ours, err := json.Marshal(hookGroup{Hooks: []groupHandler{handler}})
		if err != nil {
			return path, err
		}
		if err := events.SetValue(e.event, append(groups, ours)); err != nil {
			return path, err
		}
	}
	if err := cfg.SetObject("hooks", events); err != nil {
		return path, err
	}
	return path, writeJSON(path, cfg)
}

// installed reports whether every kemory hook is in the agent's hooks file.
func (g groupedHooks) installed() bool {
	_, events, err := readHooks(g.path())
	if err != nil {
		return false
	}
	for _, e := range g.events {
		raw, _ := events.Get(e.event)
		if _, found, err := withoutKemory(raw, g.agent); err != nil || !found {
			return false
		}
	}
	return true
}

// InstallCodexHooks adds kemory's hooks to Codex's hooks file.
func InstallCodexHooks(launch Launch) (string, error) { return codexHooks.install(launch) }

// CodexHooksInstalled reports whether every kemory hook is in Codex's hooks
// file.
func CodexHooksInstalled() bool { return codexHooks.installed() }

// InstallGeminiHooks adds kemory's hooks to Gemini CLI's settings file.
func InstallGeminiHooks(launch Launch) (string, error) { return geminiHooks.install(launch) }

// GeminiHooksInstalled reports whether every kemory hook is in Gemini CLI's
// settings file.
func GeminiHooksInstalled() bool { return geminiHooks.installed() }
