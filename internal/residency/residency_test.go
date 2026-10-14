package residency

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func refusalBody(api, dashboard string) []byte {
	hosts := "null"
	if api != "" || dashboard != "" {
		hosts = `{"api": "` + api + `", "dashboard": "` + dashboard + `"}`
	}
	return []byte(`{"detail": {"code": "ORG_WRONG_RESIDENCY_CELL", "this_cell": "zurich", "target_hosts": ` + hosts + `}}`)
}

func TestOnlyTheExactRefusalCounts(t *testing.T) {
	if ParseRefusal(401, refusalBody("", "")) != nil {
		t.Fatal("non-403 parsed as refusal")
	}
	for _, body := range []string{`{"detail": "Forbidden"}`, `{"detail": {"code": "OTHER"}}`, `not json`} {
		if ParseRefusal(403, []byte(body)) != nil {
			t.Fatalf("plain 403 parsed as refusal: %s", body)
		}
	}
	r := ParseRefusal(403, refusalBody("", ""))
	if r == nil || r.ThisCell != "zurich" || r.TargetAPI != "" {
		t.Fatalf("refusal without targets: %+v", r)
	}
}

func TestAllowlistedTargetIsKeptAndNormalized(t *testing.T) {
	r := ParseRefusal(403, refusalBody("https://API.kemory.in.sekondbrain.ai/", "https://kemory.in.sekondbrain.ai"))
	if r.TargetAPI != "https://api.kemory.in.sekondbrain.ai" || r.TargetDashboard != "https://kemory.in.sekondbrain.ai" {
		t.Fatalf("targets: %+v", r)
	}
}

func TestUnsafeTargetsAreDropped(t *testing.T) {
	for _, api := range []string{
		"http://api.kemory.in.sekondbrain.ai",
		"https://api.kemory.in.sekondbrain.ai:8443",
		"https://user@api.kemory.in.sekondbrain.ai",
		"https://api.kemory.in.sekondbrain.ai/path",
		"https://api.kemory.in.sekondbrain.ai/?q=1",
		"https://evil.example.com",
		"https://api.kemory.in.sekondbrain.ai.evil.com",
		"https://api.kemory.in.sekondbrain.ai:99999",
	} {
		if r := ParseRefusal(403, refusalBody(api, "https://evil.example.com")); r.TargetAPI != "" || r.TargetDashboard != "" {
			t.Fatalf("unsafe target kept for %s: %+v", api, r)
		}
	}
}

func TestCandidateHosts(t *testing.T) {
	zurich, mumbai := KnownCellAPIHosts["zurich"], KnownCellAPIHosts["mumbai"]
	got := CandidateHosts(&Refusal{ThisCell: "zurich", TargetAPI: mumbai}, zurich)
	if len(got) != 1 || got[0] != mumbai {
		t.Fatalf("server target should be the only candidate: %v", got)
	}
	got = CandidateHosts(&Refusal{ThisCell: "zurich"}, zurich+"/")
	if len(got) != 1 || got[0] != mumbai {
		t.Fatalf("without a target, every other cell: %v", got)
	}
	// The refusing cell is excluded by name even when the client was elsewhere.
	got = CandidateHosts(&Refusal{ThisCell: "mumbai"}, "https://kemory.example.com")
	if len(got) != 1 || got[0] != zurich {
		t.Fatalf("refusing cell must be excluded by name: %v", got)
	}
}

func TestWrongRegionMessageListsCellsWhenTheTargetIsUnknown(t *testing.T) {
	msg := WrongRegionMessage(&Refusal{ThisCell: "zurich"}, "https://api.kemory.s9n.ai")
	if !strings.Contains(msg, "mumbai: https://api.kemory.in.sekondbrain.ai") || !strings.Contains(msg, "KEMORY_URL") {
		t.Fatalf("message: %s", msg)
	}
}

func TestSelectionOverridesDiscovery(t *testing.T) {
	if SelectionOverridesDiscovery("", "org") || SelectionOverridesDiscovery("org", "org") {
		t.Fatal("no selection, or selecting the token org, must not override")
	}
	if !SelectionOverridesDiscovery("other", "org") {
		t.Fatal("a different selected org must override")
	}
}

func discoveryServer(t *testing.T, cellBody, directoryBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/residency/cell":
			if r.Header.Get("Authorization") != "Bearer t" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(cellBody))
		case "/.well-known/kemory-cells":
			if directoryBody == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(directoryBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestDiscoverHomeCell(t *testing.T) {
	headers := http.Header{"Authorization": {"Bearer t"}}
	cases := []struct {
		name, cell, directory, want string
	}{
		{"home cell", `{"is_home_cell": true}`, "", ""},
		{"allowlisted other cell", `{"is_home_cell": false, "cell_hosts": {"api": "https://api.kemory.in.sekondbrain.ai"}}`, "", "https://api.kemory.in.sekondbrain.ai"},
		{"unknown host", `{"is_home_cell": false, "cell_hosts": {"api": "https://new.example.com"}}`, "", ""},
		{"host from the cell directory", `{"is_home_cell": false, "cell_hosts": {"api": "https://new.example.com"}}`, `{"cells": {"new": {"api": "https://new.example.com"}}}`, "https://new.example.com"},
		{"malformed", `{"is_home_cell": false, "cell_hosts": {"api": "http://api.kemory.in.sekondbrain.ai"}}`, "", ""},
		{"missing flag", `{}`, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := discoveryServer(t, c.cell, c.directory)
			defer srv.Close()
			if got := DiscoverHomeCell(srv.URL, headers); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
	srv := discoveryServer(t, "", "")
	defer srv.Close()
	if got := DiscoverHomeCell(srv.URL, http.Header{}); got != "" {
		t.Fatalf("a 401 must mean stay put, got %q", got)
	}
}
