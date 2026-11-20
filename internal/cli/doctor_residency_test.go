package cli

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

const (
	zurichAPI = "https://api.kemory.s9n.ai"
	mumbaiAPI = "https://api.kemory.in.sekondbrain.ai"
)

// cellRouter answers every request in-process, keyed by host, and records the
// hosts it was asked for. A host without a handler refuses the connection, so
// no test dials out.
type cellRouter struct {
	handlers map[string]func(*http.Request) (int, string)
	asked    map[string]int
}

type refusedConnection struct{ host string }

func (e *refusedConnection) Error() string { return "dial " + e.host + ": connection refused" }

func (c *cellRouter) RoundTrip(r *http.Request) (*http.Response, error) {
	c.asked[r.URL.Host]++
	handler, ok := c.handlers[r.URL.Host]
	if !ok {
		return nil, &refusedConnection{host: r.URL.Host}
	}
	status, body := handler(r)
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

// routeCells swaps api.HTTPClient for a router that serves the given handlers.
func routeCells(t *testing.T, handlers map[string]func(*http.Request) (int, string)) *cellRouter {
	t.Helper()
	router := &cellRouter{handlers: handlers, asked: map[string]int{}}
	old := api.HTTPClient
	api.HTTPClient = &http.Client{Transport: router}
	t.Cleanup(func() { api.HTTPClient = old })
	return router
}

func answer(status int, body string) func(*http.Request) (int, string) {
	return func(*http.Request) (int, string) { return status, body }
}

func refusalNaming(target string) string {
	hosts := "null"
	if target != "" {
		hosts = `{"api": "` + target + `"}`
	}
	return `{"detail": {"code": "ORG_WRONG_RESIDENCY_CELL", "this_cell": "zurich", "target_hosts": ` + hosts + `}}`
}

// savedRoundTrip saves credentials pointing at kemoryURL, runs doctor's
// round-trip check against that URL and returns the check, after asserting
// that doctor left the saved URL alone.
func savedRoundTrip(t *testing.T, kemoryURL string) checkResult {
	t.Helper()
	setHome(t)
	plainOutput(t)
	creds := &config.Credentials{AccessToken: "at", KemoryURL: kemoryURL, Env: "prod"}
	if err := creds.Save(); err != nil {
		t.Fatal(err)
	}
	d := &doctor{}
	d.checkRoundTrip(creds, kemoryURL)
	if len(d.checks) != 1 || d.checks[0].Check != "auth round-trip" {
		t.Fatalf("checks = %+v, want exactly the auth round-trip", d.checks)
	}
	if saved, _ := config.LoadCredentials("prod"); saved == nil || saved.KemoryURL != kemoryURL {
		t.Fatalf("doctor changed the saved URL: %+v", saved)
	}
	return d.checks[0]
}

func TestDoctorReportsTheSwitchWhenTheTargetDoesNotAnswer(t *testing.T) {
	router := routeCells(t, map[string]func(*http.Request) (int, string){
		"api.kemory.s9n.ai": answer(http.StatusForbidden, refusalNaming(mumbaiAPI)),
	})

	got := savedRoundTrip(t, zurichAPI)

	wantRoundTrip(t, got, render.Fail, "403 — this org's data lives in a different residency cell (this CLI points at "+zurichAPI+
		", the 'zurich' cell). The next command switches this CLI to "+mumbaiAPI+" and saves it, but "+mumbaiAPI+
		" did not answer GET /api/v1/me (Get \""+mumbaiAPI+"/api/v1/me\": dial api.kemory.in.sekondbrain.ai: connection refused).")
	if strings.Contains(*got.Detail, "No allowlisted cell could confirm it") {
		t.Fatalf("detail contradicts what a command does: %s", *got.Detail)
	}
	if n := router.asked["api.kemory.in.sekondbrain.ai"]; n != 1 {
		t.Fatalf("target requested %d times, want 1", n)
	}
}

func TestDoctorReportsTheStatusWhenTheTargetAnswersWithOne(t *testing.T) {
	routeCells(t, map[string]func(*http.Request) (int, string){
		"api.kemory.s9n.ai":            answer(http.StatusForbidden, refusalNaming(mumbaiAPI)),
		"api.kemory.in.sekondbrain.ai": answer(http.StatusServiceUnavailable, ""),
	})

	got := savedRoundTrip(t, zurichAPI)

	wantRoundTrip(t, got, render.Fail, "403 — this org's data lives in a different residency cell (this CLI points at "+zurichAPI+
		", the 'zurich' cell). The next command switches this CLI to "+mumbaiAPI+" and saves it, but "+mumbaiAPI+
		" did not answer GET /api/v1/me (status 503).")
}

func TestDoctorPassesWhenTheTargetAnswers(t *testing.T) {
	router := routeCells(t, map[string]func(*http.Request) (int, string){
		"api.kemory.s9n.ai":            answer(http.StatusForbidden, refusalNaming(mumbaiAPI)),
		"api.kemory.in.sekondbrain.ai": answer(http.StatusOK, meBody),
	})

	got := savedRoundTrip(t, zurichAPI)

	wantRoundTrip(t, got, render.Pass, "org=org-1, teams=1 — served by "+mumbaiAPI+". This CLI's saved URL still points at "+
		zurichAPI+" (the 'zurich' cell); the next command switches and saves it.")
	if n := router.asked["api.kemory.in.sekondbrain.ai"]; n != 1 {
		t.Fatalf("target requested %d times, want 1", n)
	}
}

func TestDoctorPassesWhenAFallbackCellAnswersOnce(t *testing.T) {
	router := routeCells(t, map[string]func(*http.Request) (int, string){
		"api.kemory.s9n.ai":            answer(http.StatusForbidden, refusalNaming("")),
		"api.kemory.in.sekondbrain.ai": answer(http.StatusOK, meBody),
	})

	got := savedRoundTrip(t, zurichAPI)

	wantRoundTrip(t, got, render.Pass, "org=org-1, teams=1 — served by "+mumbaiAPI+". This CLI's saved URL still points at "+
		zurichAPI+" (the 'zurich' cell); the next command switches and saves it.")
	if n := router.asked["api.kemory.in.sekondbrain.ai"]; n != 1 {
		t.Fatalf("fallback cell requested %d times, want 1", n)
	}
}

func TestDoctorFailsWhenNoFallbackCellAnswers(t *testing.T) {
	routeCells(t, map[string]func(*http.Request) (int, string){
		"api.kemory.s9n.ai":            answer(http.StatusForbidden, refusalNaming("")),
		"api.kemory.in.sekondbrain.ai": answer(http.StatusForbidden, refusalNaming("")),
	})

	got := savedRoundTrip(t, zurichAPI)

	wantRoundTrip(t, got, render.Fail, "403 — this org's data lives in a different residency cell (this CLI points at "+zurichAPI+
		", the 'zurich' cell). No allowlisted cell could confirm it.")
}

func TestDoctorSaysASelfHostedURLIsLeftUntouched(t *testing.T) {
	router := routeCells(t, map[string]func(*http.Request) (int, string){
		"kemory.example.com":           answer(http.StatusForbidden, refusalNaming(mumbaiAPI)),
		"api.kemory.in.sekondbrain.ai": answer(http.StatusOK, meBody),
	})

	got := savedRoundTrip(t, "https://kemory.example.com")

	wantRoundTrip(t, got, render.Fail, "403 — this org's data lives in a different residency cell (this CLI points at "+
		"https://kemory.example.com, the 'zurich' cell; the org is served by "+mumbaiAPI+
		"). This URL is not a known Kemory cell, so commands leave it untouched.")
	if n := router.asked["api.kemory.in.sekondbrain.ai"]; n != 0 {
		t.Fatalf("a self-hosted token was sent to an allowlisted cell %d times", n)
	}
}
