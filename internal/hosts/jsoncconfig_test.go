package hosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const zedSettings = `// Zed settings
//
// For information on how to configure Zed, see the Zed
// documentation: https://zed.dev/docs/configuring-zed
{
  "theme": "One Dark", // the theme
  "proxy": "http://localhost:8080",
  /* fonts */
  "buffer_font_size": 15,
  "languages": { "Go": { "tab_size": 4, }, },
}
`

func TestZedKeepsCommentsAndOtherSettings(t *testing.T) {
	setHome(t)
	path := ResolveConfig("zed")
	if !strings.HasSuffix(path, "settings.json") {
		t.Fatalf("path: %s", path)
	}
	writeTestFile(t, path, zedSettings)
	if err := WriteEntry("zed", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	for _, want := range []string{"// Zed settings", "// the theme", "/* fonts */", `"proxy": "http://localhost:8080"`, `"tab_size": 4, }`} {
		if !strings.Contains(got, want) {
			t.Fatalf("lost %q:\n%s", want, got)
		}
	}
	if names := KemoryEntries("zed", path); len(names) != 1 || names[0] != "kemory" {
		t.Fatalf("entries: %v\n%s", names, got)
	}
	if c := EntryCommand("zed", path, "kemory"); c != "kemory" {
		t.Fatalf("command: %q", c)
	}

	// A second write replaces the entry in place.
	if err := WriteEntry("zed", path, "kemory", Launch{Command: "/opt/kemory", Args: []string{"mcp", "serve"}}); err != nil {
		t.Fatal(err)
	}
	if again := read(t, path); strings.Count(again, `"kemory"`) != 1 || !strings.Contains(again, "/opt/kemory") {
		t.Fatalf("replace:\n%s", again)
	}

	// Removing it leaves the user's file, comments and all.
	if err := RemoveEntries("zed", path, []string{"kemory"}); err != nil {
		t.Fatal(err)
	}
	left := read(t, path)
	if strings.Contains(left, "kemory") || !strings.Contains(left, "// the theme") {
		t.Fatalf("after remove:\n%s", left)
	}
}

func TestZedAddsBesideOtherContextServers(t *testing.T) {
	setHome(t)
	path := ResolveConfig("zed")
	writeTestFile(t, path, `{
  // servers
  "context_servers": {
    "github": { "command": "gh-mcp", "args": [], },
  },
}`)
	if err := WriteEntry("zed", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if err := RemoveEntries("zed", path, []string{"kemory"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, `"github"`) || strings.Contains(got, "kemory") || !strings.Contains(got, "// servers") {
		t.Fatalf("got:\n%s", got)
	}
	if _, err := decodeJSONC(got); err != nil {
		t.Fatalf("left invalid JSONC:\n%s", got)
	}
}

func TestZedRefusesAFileItCannotRead(t *testing.T) {
	setHome(t)
	path := ResolveConfig("zed")
	writeTestFile(t, path, `{ "theme": `)
	if err := WriteEntry("zed", path, "kemory", LaunchFor("prod")); err == nil {
		t.Fatal("wrote into a broken file")
	}
	if read(t, path) != `{ "theme": ` {
		t.Fatal("changed a broken file")
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
