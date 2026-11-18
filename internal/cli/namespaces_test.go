package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
)

func TestNamespacesSummaryPrintsSummary(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/namespaces/alpha/summary": jsonReply(200, `{"summary": "short form", "consolidated_summary": "long form"}`),
	})
	code, out, _ := run(t, "namespaces", "summary", "alpha")
	if code != 0 || !strings.Contains(out, "short form") || strings.Contains(out, "long form") {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestNamespacesSummaryFallsBackToConsolidatedSummary(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/namespaces/alpha/summary": jsonReply(200, `{"summary": "", "consolidated_summary": "long form"}`),
	})
	code, out, _ := run(t, "namespaces", "summary", "alpha")
	if code != 0 || !strings.Contains(out, "long form") {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestNamespacesSummaryEmptyState(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/namespaces/alpha/summary": jsonReply(200, `{}`),
	})
	code, out, _ := run(t, "namespaces", "summary", "alpha")
	if code != 0 || !strings.Contains(out, "alpha has no consolidated summary yet") {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestNamespacesSummaryJSONPrintsRawBody(t *testing.T) {
	setHome(t)
	const body = `{"summary": "", "consolidated_summary": "long form", "extra": 1}`
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/namespaces/alpha/summary": jsonReply(200, body),
	})
	code, out, _ := run(t, "namespaces", "summary", "alpha", "--json")
	if code != 0 || compactJSON(t, out) != compactJSON(t, body) {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestNamespacesSummaryUnknownNamespaceIsNotFound(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/namespaces/alpha/summary": jsonReply(404, `{"detail": "not found"}`),
	})
	if code, _, _ := run(t, "namespaces", "summary", "alpha"); code != exits.NotFound {
		t.Fatalf("got exit %d, want %d", code, exits.NotFound)
	}
}

func TestNamespacesEmptyList(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/namespaces": jsonReply(200, `[]`),
	})
	code, out, _ := run(t, "namespaces")
	if code != 0 || !strings.Contains(out, "no namespaces yet") {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestNamespacesJSONPrintsRawBody(t *testing.T) {
	setHome(t)
	const body = `[{"namespace": "small", "count": 1}, {"namespace": "big", "count": 90}]`
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/namespaces": jsonReply(200, body),
	})
	code, out, _ := run(t, "namespaces", "--json")
	if code != 0 || compactJSON(t, out) != compactJSON(t, body) {
		t.Fatalf("got %d %q", code, out)
	}
}

// compactJSON strips insignificant whitespace; the command re-indents the raw body.
func compactJSON(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(s)); err != nil {
		t.Fatalf("not JSON: %q: %v", s, err)
	}
	return b.String()
}
