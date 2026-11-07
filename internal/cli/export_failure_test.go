package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestExportFailureKeepsTheExistingFile(t *testing.T) {
	home := setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories/export": jsonReply(500, `{"detail": "boom"}`),
	})
	path := filepath.Join(home, "vault.json")
	if err := os.WriteFile(path, []byte(`[{"memory_id": "old"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _ := run(t, "export", "-o", path)
	data, _ := os.ReadFile(path)
	if code == 0 || string(data) != `[{"memory_id": "old"}]` {
		t.Fatalf("exit %d, file now %q", code, data)
	}
	entries, _ := os.ReadDir(home)
	for _, e := range entries {
		if e.Name() != "vault.json" && filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}
