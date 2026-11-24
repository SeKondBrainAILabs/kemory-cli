package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestExportToSymlinkedOutKeepsTheLink(t *testing.T) {
	home := setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories/export": jsonReply(200, `[{"memory_id": "new"}]`),
	})
	backup := filepath.Join(home, "backup")
	if err := os.Mkdir(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(backup, "vault.json")
	if err := os.WriteFile(target, []byte(`[{"memory_id": "old"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "vault.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if code, _, errOut := run(t, "export", "-o", link); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is no longer a symlink (mode %v)", link, info.Mode())
	}
	if data, _ := os.ReadFile(target); string(data) != `[{"memory_id": "new"}]` {
		t.Errorf("link target holds %q, want the new export", data)
	}
}
