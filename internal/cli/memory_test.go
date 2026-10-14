package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
)

const memID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

// fakeVault records each request body by path and answers with replies[path].
type fakeVault struct {
	mu      sync.Mutex
	bodies  map[string]map[string]any
	methods map[string]string
	replies map[string]func(w http.ResponseWriter)
}

func newFakeVault(t *testing.T, replies map[string]func(w http.ResponseWriter)) *fakeVault {
	t.Helper()
	v := &fakeVault{bodies: map[string]map[string]any{}, methods: map[string]string{}, replies: replies}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		v.mu.Lock()
		v.bodies[r.URL.Path] = body
		v.methods[r.URL.Path] = r.Method
		v.mu.Unlock()
		reply, ok := v.replies[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		reply(w)
	}))
	t.Cleanup(srv.Close)
	saveCreds(t, srv.URL)
	return v
}

func jsonReply(status int, body string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func withStdin(t *testing.T, input string) {
	t.Helper()
	oldIn, oldTTY := Stdin, stdinIsTerminal
	Stdin = strings.NewReader(input)
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { Stdin, stdinIsTerminal = oldIn, oldTTY })
}

func atTerminal(t *testing.T) {
	t.Helper()
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminal = old })
}

func TestRememberStoresAndSaysWhere(t *testing.T) {
	setHome(t)
	atTerminal(t)
	v := newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories": jsonReply(201, `{"memory_id": "`+memID+`", "namespace": "project:kemory"}`),
	})
	code, out, _ := run(t, "remember", "we", "deploy", "with", "ArgoCD", "--tag", "deploy", "--tag", "ops", "-n", "project:kemory", "--ttl", "3600")
	if code != 0 || !strings.Contains(out, "Stored in project:kemory") || !strings.Contains(out, memID) {
		t.Fatalf("got %d %q", code, out)
	}
	body := v.bodies["/api/v1/memories"]
	if body["content"] != "we deploy with ArgoCD" || body["namespace"] != "project:kemory" || body["ttl_seconds"].(float64) != 3600 {
		t.Fatalf("body: %v", body)
	}
	if tags := body["metadata"].(map[string]any)["tags"].([]any); len(tags) != 2 {
		t.Fatalf("tags: %v", tags)
	}
	if _, ok := body["allow_duplicate"]; ok {
		t.Fatal("allow_duplicate sent without --force")
	}
}

func TestRememberReadsAPipe(t *testing.T) {
	setHome(t)
	withStdin(t, "  from a pipe\n")
	v := newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories": jsonReply(201, `{"memory_id": "x", "namespace": "project:general"}`),
	})
	if code, _, _ := run(t, "remember"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if v.bodies["/api/v1/memories"]["content"] != "from a pipe" {
		t.Fatalf("body: %v", v.bodies["/api/v1/memories"])
	}
}

func TestRememberWithNothingToStoreIsAUsageError(t *testing.T) {
	setHome(t)
	atTerminal(t)
	newFakeVault(t, nil)
	if code, _, _ := run(t, "remember"); code != exits.Usage {
		t.Fatalf("got %d", code)
	}
}

func TestRememberReportsADuplicateInsteadOfStored(t *testing.T) {
	setHome(t)
	atTerminal(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories": jsonReply(201, `{"memory_id": "x", "namespace": "n", "dedup": {"deduplicated": true, "kind": "semantic", "similarity": 0.97}}`),
	})
	code, out, errOut := run(t, "remember", "again")
	if code != 0 || strings.Contains(out, "Stored") || !strings.Contains(errOut, "Already known (semantic, similarity 0.97)") {
		t.Fatalf("got %d %q %q", code, out, errOut)
	}
}

func TestRememberNamespaceConflictExitsConflict(t *testing.T) {
	setHome(t)
	atTerminal(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories": jsonReply(409, `{"detail": {"requested": "project:kemry", "suggested": [{"namespace": "project:kemory", "similarity": 0.94}]}}`),
	})
	code, _, errOut := run(t, "remember", "x", "-n", "project:kemry")
	if code != exits.Conflict || !strings.Contains(errOut, "project:kemory (0.94)") || !strings.Contains(errOut, "--force") {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestRecallTableAndHonestCounts(t *testing.T) {
	setHome(t)
	v := newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories/search": jsonReply(200, `{"items": [{"memory_id": "`+memID+`", "namespace": "project:kemory", "created_at": "2026-10-01T10:00:00Z", "content": "line one\nline two"}], "total": 1, "total_mode": "page"}`),
	})
	code, out, _ := run(t, "recall", "residency", "cell", "--limit", "5", "--mode", "dense")
	if code != 0 || !strings.Contains(out, memID) || !strings.Contains(out, "line one line two") || !strings.Contains(out, "top-K") {
		t.Fatalf("got %d\n%s", code, out)
	}
	body := v.bodies["/api/v1/memories/search"]
	if body["query"] != "residency cell" || body["search_mode"] != "dense" || body["limit"].(float64) != 5 {
		t.Fatalf("body: %v", body)
	}
}

func TestRecallValidatesFlags(t *testing.T) {
	setHome(t)
	newFakeVault(t, nil)
	for _, args := range [][]string{
		{"recall", "x", "--mode", "fuzzy"},
		{"recall", "x", "--limit", "500"},
		{"recall", "x", "--min-relevance", "2"},
		{"recall"},
	} {
		if code, _, _ := run(t, args...); code != exits.Usage {
			t.Fatalf("%v: got %d", args, code)
		}
	}
}

func TestRecallEncryptedVaultOutageIsNotAnEmptyResult(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories/search": func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusServiceUnavailable)
		},
	})
	code, _, errOut := run(t, "recall", "x")
	if code != exits.Unavailable || !strings.Contains(errOut, "Retry in 30s") {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestRecallSaysWhenAnEmptyResultIsInconclusive(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories/search": jsonReply(200, `{"items": [], "no_results_inconclusive": true}`),
	})
	_, _, errOut := run(t, "recall", "x")
	if !strings.Contains(errOut, "Nothing matched") || !strings.Contains(errOut, "inconclusive rather than empty") {
		t.Fatalf("got %q", errOut)
	}
}

func TestLsSendsNoQueryAndJSONKeepsServerOrder(t *testing.T) {
	setHome(t)
	v := newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories/search": jsonReply(200, `{"zeta": 1, "items": [], "alpha": 2}`),
	})
	code, out, _ := run(t, "ls", "--json")
	if code != 0 || strings.Index(out, "zeta") > strings.Index(out, "alpha") {
		t.Fatalf("got %d %q", code, out)
	}
	if _, ok := v.bodies["/api/v1/memories/search"]["query"]; ok || v.bodies["/api/v1/memories/search"]["search_mode"] != "hybrid" {
		t.Fatalf("body: %v", v.bodies["/api/v1/memories/search"])
	}
}

func TestShowAndItsFailures(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories/" + memID:              jsonReply(200, `{"memory_id": "`+memID+`", "namespace": "n", "content": "full text", "metadata": {"tags": ["a", "b"]}, "version": 3}`),
		"/api/v1/memories/" + memID + "/history": jsonReply(200, `[{"created_at": "2026-10-01T10:00:00Z", "event_type": "created", "actor_type": "user"}]`),
		"/api/v1/memories/not-a-uuid":            jsonReply(422, `{"detail": []}`),
	})
	code, out, _ := run(t, "show", memID, "--history")
	for _, want := range []string{"full text", "a, b", "History", "created"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q (exit %d):\n%s", want, code, out)
		}
	}
	if code, _, errOut := run(t, "show", "not-a-uuid"); code != exits.Usage || !strings.Contains(errOut, "full UUIDs") {
		t.Fatalf("422: %d %q", code, errOut)
	}
	if code, _, _ := run(t, "show", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"); code != exits.NotFound {
		t.Fatalf("404: %d", code)
	}
}

func TestForgetAsksFirst(t *testing.T) {
	setHome(t)
	v := newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories/" + memID: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"memory_id": "`+memID+`", "namespace": "n", "content": "doomed"}`)
		},
	})
	withStdin(t, "n\n")
	code, out, _ := run(t, "forget", memID)
	if code != 0 || !strings.Contains(out, "doomed") || v.methods["/api/v1/memories/"+memID] != http.MethodGet {
		t.Fatalf("declining must not delete: %d %q %v", code, out, v.methods)
	}
	withStdin(t, "y\n")
	if code, out, _ := run(t, "forget", memID); code != 0 || !strings.Contains(out, "Deleted") {
		t.Fatalf("got %d %q", code, out)
	}
	if v.methods["/api/v1/memories/"+memID] != http.MethodDelete {
		t.Fatal("not deleted")
	}
}

func TestNamespacesSortedBySize(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/namespaces": jsonReply(200, `[{"namespace": "small", "count": 1}, {"namespace": "big", "count": 90}]`),
	})
	_, out, _ := run(t, "namespaces")
	if strings.Index(out, "big") > strings.Index(out, "small") {
		t.Fatalf("not sorted:\n%s", out)
	}
}

func TestExportToFileAndStdout(t *testing.T) {
	home := setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/memories/export": jsonReply(200, `[{"memory_id": "1"}, {"memory_id": "2"}]`),
	})
	path := filepath.Join(home, "vault.json")
	code, out, _ := run(t, "export", "-o", path)
	data, _ := os.ReadFile(path)
	if code != 0 || !strings.Contains(out, "Wrote 40 bytes") || len(data) != 40 {
		t.Fatalf("got %d %q, file %q", code, out, data)
	}
	if _, out, _ := run(t, "export"); out != `[{"memory_id": "1"}, {"memory_id": "2"}]` {
		t.Fatalf("stdout: %q", out)
	}
}

func TestAsk(t *testing.T) {
	setHome(t)
	v := newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/ask": jsonReply(200, `{"synthesized": true, "answer": "## Decision\nUse hybrid search.", "items": [{"type": "chat", "title": "Search architecture", "snippet": "we chose hybrid"}]}`),
	})
	code, out, _ := run(t, "ask", "what", "did", "we", "decide", "--type", "chat", "--no-synth")
	if code != 0 || !strings.Contains(out, "Use hybrid search.") || !strings.Contains(out, "Search architecture") {
		t.Fatalf("got %d\n%s", code, out)
	}
	body := v.bodies["/api/v1/ask"]
	if body["query"] != "what did we decide" || body["synthesize"] != false {
		t.Fatalf("body: %v", body)
	}
	if code, _, _ := run(t, "ask", "x", "--type", "email"); code != exits.Usage {
		t.Fatalf("bad --type: %d", code)
	}
}
