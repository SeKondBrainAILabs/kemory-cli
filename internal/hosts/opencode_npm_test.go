package hosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The opencode-kemory package is generated from the plugin kemory connect
// writes. Run `UPDATE_OPENCODE_PLUGIN=1 go test ./internal/hosts` to
// regenerate it.
func TestOpenCodeNPMPluginIsCurrent(t *testing.T) {
	path := filepath.Join("..", "..", "packaging", "opencode", "index.js")
	want := OpenCodeNPMPlugin()
	if os.Getenv("UPDATE_OPENCODE_PLUGIN") == "1" {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout may turn the line endings into CRLF.
	lf := func(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
	if lf(string(got)) != lf(want) {
		t.Fatal("packaging/opencode/index.js is stale: run UPDATE_OPENCODE_PLUGIN=1 go test ./internal/hosts")
	}
	for _, unwanted := range []string{commandPlaceholder, "Written by `kemory connect`"} {
		if strings.Contains(want, unwanted) {
			t.Fatalf("the npm plugin still has %q", unwanted)
		}
	}
}
