package hosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveEntriesKeepsSymlinkAndClearsTarget(t *testing.T) {
	home := setHome(t)
	target := filepath.Join(home, "dotfiles", "cursor-mcp.json")
	mustWrite(t, target, `{"mcpServers": {"kemory": {"command": "npx"}}}`)
	link := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := RemoveEntries("cursor", link, []string{"kemory"}); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link should still be a symlink: info=%v err=%v", info, err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "kemory") {
		t.Errorf("target still holds the kemory entry: %s", body)
	}
}
