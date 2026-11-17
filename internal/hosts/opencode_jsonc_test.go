package hosts

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const opencodeJSONC = `{
  // the model I use every day
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-5-5",
}
`

func opencodeJSONCPath(t *testing.T) string {
	t.Helper()
	home := setHome(t)
	path := filepath.Join(home, ".config", "opencode", "opencode.jsonc")
	mustWrite(t, path, opencodeJSONC)
	return path
}

// OpenCode reads opencode.jsonc as JSON with comments, and ResolveConfig
// picks that file when it is the only one. kemory must then be able to add
// its entry, keeping the user's comments.
func TestOpenCodeJSONCConfigWithCommentsAcceptsTheKemoryEntry(t *testing.T) {
	path := opencodeJSONCPath(t)
	if got := ResolveConfig("opencode"); got != path {
		t.Fatalf("ResolveConfig = %s, want %s", got, path)
	}
	if err := WriteEntry("opencode", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	if got := KemoryEntries("opencode", path); !reflect.DeepEqual(got, []string{"kemory"}) {
		t.Fatalf("KemoryEntries = %v", got)
	}
	if !strings.Contains(read(t, path), "// the model I use every day") {
		t.Fatalf("the user's comment was dropped:\n%s", read(t, path))
	}
}

func TestOpenCodeJSONCEntryHasTheOpenCodeShape(t *testing.T) {
	path := opencodeJSONCPath(t)
	if err := WriteEntry("opencode", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	text := read(t, path)
	for _, want := range []string{`"type": "local"`, `"enabled": true`, `"mcp"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"args"`) {
		t.Fatalf("got the plain command/args shape:\n%s", text)
	}
}

func TestOpenCodeJSONCEntryCommandIsRead(t *testing.T) {
	path := opencodeJSONCPath(t)
	l := LaunchFor("prod")
	if err := WriteEntry("opencode", path, "kemory", l); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	if got := EntryCommand("opencode", path, "kemory"); got != l.Command {
		t.Fatalf("EntryCommand = %q, want %q", got, l.Command)
	}
}

func TestOpenCodeJSONCRemoveEntriesKeepsComments(t *testing.T) {
	path := opencodeJSONCPath(t)
	if err := WriteEntry("opencode", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	if err := RemoveEntries("opencode", path, []string{"kemory"}); err != nil {
		t.Fatalf("RemoveEntries: %v", err)
	}
	if got := KemoryEntries("opencode", path); len(got) != 0 {
		t.Fatalf("entry still there: %v", got)
	}
	if !strings.Contains(read(t, path), "// the model I use every day") {
		t.Fatalf("the user's comment was dropped:\n%s", read(t, path))
	}
}
