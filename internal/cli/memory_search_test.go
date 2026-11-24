package cli

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
)

const searchPath = "/api/v1/memories/search"

const twoItems = `[{"memory_id": "` + memID + `", "namespace": "project:kemory", "created_at": "2026-10-01T10:00:00Z", "content": "one"},
	{"memory_id": "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "namespace": "project:kemory", "created_at": "2026-10-02T10:00:00Z", "content": "two"}]`

// searchVault answers every search with an empty page and records the body.
func searchVault(t *testing.T) *fakeVault {
	t.Helper()
	return newFakeVault(t, map[string]func(http.ResponseWriter){
		searchPath: jsonReply(200, `{"items": []}`),
	})
}

func assertSearchBody(t *testing.T, v *fakeVault, want map[string]any) {
	t.Helper()
	got, ok := v.bodies[searchPath]
	if !ok {
		t.Fatalf("no request reached %s", searchPath)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("search body:\n got %v\nwant %v", got, want)
	}
}

func hasLine(out, want string) bool {
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}

func TestRecallSendsEveryFilterInTheSearchBody(t *testing.T) {
	setHome(t)
	v := searchVault(t)
	code, _, errOut := run(t, "recall", "x", "-n", "project:kemory", "--type", "note", "--tag", "a", "--tag", "b",
		"--min-relevance", "0.4", "--limit", "5", "--offset", "10", "--mode", "dense")
	if code != 0 {
		t.Fatalf("got %d %q", code, errOut)
	}
	assertSearchBody(t, v, map[string]any{
		"query": "x", "search_mode": "dense", "limit": float64(5), "offset": float64(10),
		"namespace": "project:kemory", "content_type": "note", "tags": []any{"a", "b"}, "min_relevance": 0.4,
	})
}

func TestRecallWithoutFiltersSendsOnlyQueryModeAndPaging(t *testing.T) {
	setHome(t)
	v := searchVault(t)
	if code, _, errOut := run(t, "recall", "x"); code != 0 {
		t.Fatalf("got %d %q", code, errOut)
	}
	assertSearchBody(t, v, map[string]any{
		"query": "x", "search_mode": "hybrid", "limit": float64(20), "offset": float64(0),
	})
}

func TestRecallSendsAZeroMinRelevanceWhenGiven(t *testing.T) {
	setHome(t)
	v := searchVault(t)
	if code, _, errOut := run(t, "recall", "x", "--min-relevance", "0"); code != 0 {
		t.Fatalf("got %d %q", code, errOut)
	}
	assertSearchBody(t, v, map[string]any{
		"query": "x", "search_mode": "hybrid", "limit": float64(20), "offset": float64(0), "min_relevance": float64(0),
	})
	if code, _, errOut := run(t, "recall", "x", "--min-relevance", "1"); code != 0 {
		t.Fatalf("got %d %q", code, errOut)
	}
	assertSearchBody(t, v, map[string]any{
		"query": "x", "search_mode": "hybrid", "limit": float64(20), "offset": float64(0), "min_relevance": float64(1),
	})
}

func TestRecallSendsACommaTagAsOneTag(t *testing.T) {
	setHome(t)
	v := searchVault(t)
	if code, _, errOut := run(t, "recall", "x", "--tag", "a,b"); code != 0 {
		t.Fatalf("got %d %q", code, errOut)
	}
	assertSearchBody(t, v, map[string]any{
		"query": "x", "search_mode": "hybrid", "limit": float64(20), "offset": float64(0), "tags": []any{"a,b"},
	})
}

func TestLsSendsItsFiltersWithoutAQuery(t *testing.T) {
	setHome(t)
	v := searchVault(t)
	if code, _, errOut := run(t, "ls", "-n", "project:kemory", "--type", "note", "--tag", "a"); code != 0 {
		t.Fatalf("got %d %q", code, errOut)
	}
	assertSearchBody(t, v, map[string]any{
		"search_mode": "hybrid", "limit": float64(20), "offset": float64(0),
		"namespace": "project:kemory", "content_type": "note", "tags": []any{"a"},
	})
}

func TestSearchRejectsANegativeOffsetBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{{"recall", "x", "--offset", "-1"}, {"ls", "--offset", "-1"}} {
		t.Run(args[0], func(t *testing.T) {
			setHome(t)
			v := searchVault(t)
			code, _, errOut := run(t, args...)
			if code != exits.Usage || !strings.Contains(errOut, "--offset must not be negative") {
				t.Fatalf("got %d %q", code, errOut)
			}
			if _, ok := v.bodies[searchPath]; ok {
				t.Fatalf("a search request was sent: %v", v.bodies[searchPath])
			}
		})
	}
}

func TestRecallOutageWithoutRetryAfter(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		searchPath: func(w http.ResponseWriter) { w.WriteHeader(http.StatusServiceUnavailable) },
	})
	code, _, errOut := run(t, "recall", "x")
	want := "Search is temporarily unavailable for an encrypted vault (a dependency is down). Retry in a few seconds — this is not an empty result."
	if code != exits.Unavailable || !strings.Contains(errOut, want) {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestListCountLineWithoutANamespaceNotesSyntheticNamespaces(t *testing.T) {
	want := "2 of 7 (excludes synthetic loadtest:/bench: namespaces; `kemory export` includes them)"
	for _, args := range [][]string{{"ls"}, {"recall", "x"}} {
		t.Run(args[0], func(t *testing.T) {
			setHome(t)
			newFakeVault(t, map[string]func(http.ResponseWriter){
				searchPath: jsonReply(200, `{"items": `+twoItems+`, "total": 7, "total_mode": "exact"}`),
			})
			code, out, errOut := run(t, args...)
			if code != 0 || !hasLine(out, want) {
				t.Fatalf("got %d %q\n%s", code, errOut, out)
			}
		})
	}
}

func TestListCountLineWithANamespaceIsBare(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		searchPath: jsonReply(200, `{"items": `+twoItems+`, "total": 7, "total_mode": "exact"}`),
	})
	code, out, errOut := run(t, "ls", "-n", "project:kemory")
	if code != 0 || !hasLine(out, "2 of 7") || strings.Contains(out, "excludes synthetic") {
		t.Fatalf("got %d %q\n%s", code, errOut, out)
	}
}

func TestListCountLineFallsBackToShownWhenTotalIsMissing(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		searchPath: jsonReply(200, `{"items": `+twoItems+`}`),
	})
	code, out, errOut := run(t, "ls", "-n", "project:kemory")
	if code != 0 || !hasLine(out, "2 of 2") {
		t.Fatalf("got %d %q\n%s", code, errOut, out)
	}
}
