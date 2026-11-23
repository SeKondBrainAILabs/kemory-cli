package hosts

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeCodexConfig(t *testing.T, body string) string {
	t.Helper()
	home := setHome(t)
	path := filepath.Join(home, ".codex", "config.toml")
	mustWrite(t, path, body)
	return path
}

func TestTOMLRemovalRefusesAKemoryServerItCannotCut(t *testing.T) {
	cases := map[string]string{
		"inline table": "model = \"m\"\nmcp_servers = { kemory = { command = \"x\" } }\n",
		"dotted keys":  "[mcp_servers]\nkemory.command = \"x\"\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeCodexConfig(t, body)
			err := RemoveEntries("codex", path, []string{"kemory"})
			if !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
			if !strings.Contains(err.Error(), "in a form kemory cannot remove") {
				t.Fatalf("err = %v, want 'in a form kemory cannot remove'", err)
			}
			if got := read(t, path); got != body {
				t.Fatalf("file changed:\n%s", got)
			}
		})
	}
}

func TestTOMLRemovalSkipsEscapedQuotesInStrings(t *testing.T) {
	body := "note = \"say \\\"[hi\"\n\n[mcp_servers.kemory]\ncommand = \"x\"\n\n[profiles.fast]\nmodel = \"mini\"\n"
	path := writeCodexConfig(t, body)
	if err := RemoveEntries("codex", path, []string{"kemory"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"note":     "say \"[hi",
		"profiles": map[string]any{"fast": map[string]any{"model": "mini"}},
	}
	if got := decodeTOML(t, path); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTOMLRemovalFindsQuotedHeaderKeys(t *testing.T) {
	cases := map[string]struct {
		body string
		want map[string]any
	}{
		"quoted key": {
			body: "model = \"m\"\n\n[mcp_servers.\"kemory\"]\ncommand = \"x\"\n\n[mcp_servers.other]\ncommand = \"o\"\n",
			want: map[string]any{
				"model":       "m",
				"mcp_servers": map[string]any{"other": map[string]any{"command": "o"}},
			},
		},
		"spaced quoted keys": {
			body: "model = \"m\"\n\n[ \"mcp_servers\" . 'kemory' ]\ncommand = \"x\"\n",
			want: map[string]any{"model": "m"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeCodexConfig(t, tc.body)
			if err := RemoveEntries("codex", path, []string{"kemory"}); err != nil {
				t.Fatal(err)
			}
			if got := decodeTOML(t, path); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
