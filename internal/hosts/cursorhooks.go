package hosts

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// Cursor runs user hooks from ~/.cursor/hooks.json: {"version": 1, "hooks":
// {event: [handler, …]}}, each handler a command. Kemory adds one handler per
// event that calls `kemory hook`, and leaves every other hook alone. Cursor
// asks no approval for user hooks.

// cursorHookEvents maps each Cursor event to the kemory hook it runs and the
// handler's timeout in seconds.
var cursorHookEvents = []struct {
	event, hook string
	timeout     int
}{
	{"sessionStart", "session-start", 10},
	{"stop", "capture", 10},
	{"preCompact", "capture", 10},
	{"sessionEnd", "capture", 10},
}

// CursorHooksPath is Cursor's user-level hooks file.
func CursorHooksPath() string {
	return filepath.Join(home(), ".cursor", "hooks.json")
}

type cursorHandler struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// cursorWithoutKemory returns an event's handlers minus kemory's own, and
// whether any were found.
func cursorWithoutKemory(raw json.RawMessage) ([]json.RawMessage, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	var handlers []json.RawMessage
	if err := json.Unmarshal(raw, &handlers); err != nil {
		return nil, false, err
	}
	var kept []json.RawMessage
	found := false
	for _, h := range handlers {
		var handler struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(h, &handler) == nil && isKemoryHook(handler.Command, "cursor") {
			found = true
			continue
		}
		kept = append(kept, h)
	}
	return kept, found, nil
}

// InstallCursorHooks adds kemory's hooks to Cursor's hooks file, replacing
// any it added before, and leaves every other hook as it was.
func InstallCursorHooks(launch Launch) (string, error) {
	path := CursorHooksPath()
	cfg, events, err := readHooks(path)
	if err != nil {
		return path, err
	}
	if _, ok := cfg.Get("version"); !ok {
		if err := cfg.SetValue("version", 1); err != nil {
			return path, err
		}
	}
	for _, e := range cursorHookEvents {
		raw, _ := events.Get(e.event)
		handlers, _, err := cursorWithoutKemory(raw)
		if err != nil {
			return path, invalid(path, fmt.Sprintf("has a %s value that is not a list of hooks", e.event))
		}
		ours, err := json.Marshal(cursorHandler{Command: hookCommand(launch, e.hook, "cursor"), Timeout: e.timeout})
		if err != nil {
			return path, err
		}
		if err := events.SetValue(e.event, append(handlers, ours)); err != nil {
			return path, err
		}
	}
	if err := cfg.SetObject("hooks", events); err != nil {
		return path, err
	}
	return path, writeJSON(path, cfg)
}

// CursorHooksInstalled reports whether every kemory hook is in Cursor's hooks
// file.
func CursorHooksInstalled() bool {
	_, events, err := readHooks(CursorHooksPath())
	if err != nil {
		return false
	}
	for _, e := range cursorHookEvents {
		raw, _ := events.Get(e.event)
		if _, found, err := cursorWithoutKemory(raw); err != nil || !found {
			return false
		}
	}
	return true
}
