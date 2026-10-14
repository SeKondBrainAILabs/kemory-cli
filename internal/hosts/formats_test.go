package hosts

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func decodeTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	doc := map[string]any{}
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func filesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestEveryHostRoundTripsWithoutLookingInstalled(t *testing.T) {
	for _, name := range Names {
		t.Run(name, func(t *testing.T) {
			home := setHome(t)
			path := ResolveConfig(name)
			if err := WriteEntry(name, path, "kemory-india", LaunchFor("india")); err != nil {
				t.Fatal(err)
			}
			if got := KemoryEntries(name, path); !reflect.DeepEqual(got, []string{"kemory-india"}) {
				t.Fatalf("entries: %v", got)
			}
			for _, other := range Names {
				if Installed(other) {
					t.Fatalf("a %s config kemory wrote made %s look installed", name, other)
				}
			}
			if err := RemoveEntries(name, path, []string{"kemory-india"}); err != nil {
				t.Fatal(err)
			}
			if files := filesUnder(t, home); len(files) != 0 {
				t.Fatalf("left behind: %v", files)
			}
		})
	}
}

func TestVSCodeEntriesAreTypedUnderServers(t *testing.T) {
	setHome(t)
	path := ResolveConfig("vscode")
	if !strings.HasSuffix(path, filepath.Join("Code", "User", "mcp.json")) {
		t.Fatalf("path: %s", path)
	}
	if err := WriteEntry("vscode", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"servers\": {\n    \"kemory\": {\n      \"type\": \"stdio\",\n      \"command\": \"kemory\",\n" +
		"      \"args\": [\n        \"--env\",\n        \"prod\",\n        \"mcp\",\n        \"serve\"\n      ]\n    }\n  }\n}\n"
	if got := read(t, path); got != want {
		t.Fatalf("got:\n%s", got)
	}
}

const codexConfig = `# Codex settings
model = "gpt-5"
notes = """
[mcp_servers.kemory]
not a table
"""
matrix = [
  [1, 2],
  [3, 4],
]

[mcp_servers.kemory]
command = "old"
args = ["x"]

[mcp_servers.kemory.env]
TOKEN = "t"

# GitHub, keep me
[mcp_servers.github]
command = "npx" # inline comment
args = ["-y", "gh"]

[profiles.fast]
model = "gpt-5-mini"
`

func TestTOMLReplacesOnlyTheKemoryTables(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".codex", "config.toml")
	mustWrite(t, path, codexConfig)
	before := decodeTOML(t, path)
	if err := WriteEntry("codex", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	first := read(t, path)
	for _, keep := range []string{"# Codex settings", "not a table", "# GitHub, keep me", "# inline comment", "[profiles.fast]"} {
		if !strings.Contains(first, keep) {
			t.Fatalf("lost %q:\n%s", keep, first)
		}
	}
	if strings.Contains(first, `"old"`) || strings.Contains(first, "TOKEN") {
		t.Fatalf("the old entry survived:\n%s", first)
	}
	if !strings.HasSuffix(first, "\n\n[mcp_servers.kemory]\ncommand = \"kemory\"\nargs = [\"--env\", \"prod\", \"mcp\", \"serve\"]\n") {
		t.Fatalf("entry not appended:\n%s", first)
	}
	after := decodeTOML(t, path)
	if !reflect.DeepEqual(without(before, "kemory"), without(after, "kemory")) {
		t.Fatal("settings other than the kemory entry changed")
	}
	if err := WriteEntry("codex", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if read(t, path) != first {
		t.Fatal("rewriting the same entry is not idempotent")
	}
	if got := KemoryEntries("codex", path); !reflect.DeepEqual(got, []string{"kemory"}) {
		t.Fatalf("entries: %v", got)
	}

	if err := RemoveEntries("codex", path, []string{"kemory"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(without(before, "kemory"), without(decodeTOML(t, path), "")) {
		t.Fatal("removal changed other settings")
	}
	if !strings.Contains(read(t, path), "# GitHub, keep me") {
		t.Fatal("removal dropped the next table's comment")
	}
}

func TestTOMLQuotesNamesThatAreNotBareKeys(t *testing.T) {
	home := setHome(t)
	path := filepath.Join(home, ".grok", "config.toml")
	if err := WriteEntry("grok", path, "kemory.dev", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(read(t, path), `[mcp_servers."kemory.dev"]`) {
		t.Fatalf("got:\n%s", read(t, path))
	}
	if err := WriteEntry("grok", path, "kemory.dev", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if got := KemoryEntries("grok", path); !reflect.DeepEqual(got, []string{"kemory.dev"}) {
		t.Fatalf("quoted header not recognised on rewrite: %v", got)
	}
}

func TestTOMLThatCannotBeEditedSafelyIsLeftAlone(t *testing.T) {
	for name, body := range map[string]string{
		"invalid":      "model = \n",
		"inline table": "mcp_servers = { kemory = { command = \"x\" } }\n",
		"dotted keys":  "[mcp_servers]\nkemory.command = \"x\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			home := setHome(t)
			path := filepath.Join(home, ".codex", "config.toml")
			mustWrite(t, path, body)
			err := WriteEntry("codex", path, "kemory", LaunchFor("prod"))
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("got %v", err)
			}
			if read(t, path) != body {
				t.Fatal("the file was changed")
			}
			if name != "invalid" && !strings.Contains(err.Error(), "[mcp_servers.kemory]") {
				t.Fatalf("the error must show the entry to add by hand: %v", err)
			}
		})
	}
}

func TestContinueGetsItsOwnFileAndKeepsTheUsersConfig(t *testing.T) {
	home := setHome(t)
	userConfig := filepath.Join(home, ".continue", "config.json")
	mustWrite(t, userConfig, `{"models": []}`)
	path := ResolveConfig("continue")
	if path != filepath.Join(home, ".continue", "mcpServers", "kemory.json") {
		t.Fatalf("path: %s", path)
	}
	if err := WriteEntry("continue", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if servers(t, path)["kemory"]["command"] != "kemory" {
		t.Fatalf("got:\n%s", read(t, path))
	}
	if !Installed("continue") {
		t.Fatal("config.json is Continue's own file")
	}
	if err := RemoveEntries("continue", path, []string{"kemory"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("the emptied mcpServers directory should be gone")
	}
	if read(t, userConfig) != `{"models": []}` {
		t.Fatal("the user's config must not change")
	}
}

func TestInstalledLooksPastDirsHoldingOnlyKemoryConfigs(t *testing.T) {
	home := setHome(t)
	if err := WriteEntry("antigravity", ResolveConfig("antigravity"), "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if Installed("gemini-cli") {
		t.Fatal("antigravity's config, inside ~/.gemini, made gemini-cli look installed")
	}
	mustWrite(t, filepath.Join(home, ".gemini", "oauth_creds.json"), "{}")
	if !Installed("gemini-cli") {
		t.Fatal("an app file was not detected")
	}

	if err := WriteEntry("cline", ResolveConfig("cline"), "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	if Installed("cline") || Installed("vscode") {
		t.Fatal("cline's settings file made cline or vscode look installed")
	}
	mustWrite(t, filepath.Join(clineExtensionDir(), "tasks", "1", "task.json"), "{}")
	if !Installed("cline") {
		t.Fatal("cline's own files were not detected")
	}
}

func TestResolveConfigPrefersAnExistingDirectory(t *testing.T) {
	home := setHome(t)
	if got := ResolveConfig("windsurf"); got != filepath.Join(home, ".config", "devin", "mcp_config.json") && got != filepath.Join(appData(), "devin", "mcp_config.json") {
		t.Fatalf("with nothing installed the documented path wins, got %s", got)
	}
	legacy := filepath.Join(home, ".codeium", "windsurf")
	mustWrite(t, filepath.Join(legacy, "installation_id"), "x")
	if got := ResolveConfig("windsurf"); got != filepath.Join(legacy, "mcp_config.json") {
		t.Fatalf("an install from before the rename keeps its dir, got %s", got)
	}
}

func TestCodexHomeMovesTheConfig(t *testing.T) {
	home := setHome(t)
	t.Setenv("CODEX_HOME", filepath.Join(home, "elsewhere"))
	if got := ResolveConfig("codex"); got != filepath.Join(home, "elsewhere", "config.toml") {
		t.Fatalf("got %s", got)
	}
}

func TestTOMLStringEscapes(t *testing.T) {
	s := "a\"b\\c\nd\te\x01é"
	var doc map[string]any
	if _, err := toml.Decode("k = "+tomlString(s), &doc); err != nil || doc["k"] != s {
		t.Fatalf("got %v %v", doc, err)
	}
}
