package cli

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/residency"
)

const loginCellB = "https://cell-b.example.test"

// loginIssuer is a fake identity provider that completes the device flow with
// the access token "at".
func loginIssuer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth/device") {
			fmt.Fprint(w, `{"device_code": "dc", "user_code": "WXYZ", "verification_uri": "https://idp.example.test/device"}`)
			return
		}
		fmt.Fprint(w, `{"access_token": "at", "refresh_token": "rt", "expires_in": 300}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// loginFirstAPI is the loopback API a login starts against. It answers the
// residency question with cellAnswer and the cell directory with directory
// (empty means 404), and records every path it was asked for.
type loginFirstAPI struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func newLoginFirstAPI(t *testing.T, cellAnswer, directory string, meStatus int) *loginFirstAPI {
	t.Helper()
	f := &loginFirstAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()
		if r.URL.Path == "/.well-known/kemory-cells" {
			if directory == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			fmt.Fprint(w, directory)
			return
		}
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/residency/cell":
			fmt.Fprint(w, cellAnswer)
		case "/api/v1/me":
			w.WriteHeader(meStatus)
			fmt.Fprint(w, meBody)
		case "/api/v1/me/orgs":
			fmt.Fprint(w, `[{"org_id": "org-1", "name": "Acme"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *loginFirstAPI) asked(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.paths {
		if p == path {
			return true
		}
	}
	return false
}

// hostTransport serves https://cell-b.example.test in-process (the home-cell
// discovery only accepts an https host without a port, so it cannot be a
// loopback server) and hands every other request to the previous transport,
// the loopback guard. With handler nil, cell-b cannot be reached.
type hostTransport struct {
	handler http.Handler
	next    http.RoundTripper
	host    string
}

func (c hostTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != c.host {
		return c.next.RoundTrip(r)
	}
	if c.handler == nil {
		return nil, errors.New(c.host + " is unreachable")
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, r)
	return rec.Result(), nil
}

func routeCellB(t *testing.T, handler http.Handler) {
	t.Helper()
	routeHost(t, "cell-b.example.test", handler)
}

// routeHost serves host in-process with handler (nil: the host cannot be
// reached) and restores both transports when the test ends.
func routeHost(t *testing.T, host string, handler http.Handler) {
	t.Helper()
	oldAPI, oldResidency := api.HTTPClient.Transport, residency.HTTPClient.Transport
	api.HTTPClient.Transport = hostTransport{handler: handler, next: oldAPI, host: host}
	residency.HTTPClient.Transport = hostTransport{handler: handler, next: oldResidency, host: host}
	t.Cleanup(func() {
		api.HTTPClient.Transport, residency.HTTPClient.Transport = oldAPI, oldResidency
	})
}

func cellBHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/me":
			fmt.Fprint(w, meBody)
		case "/api/v1/me/orgs":
			fmt.Fprint(w, `[{"org_id": "org-1", "name": "Acme"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

const (
	loginCellBAnswer    = `{"is_home_cell": false, "cell_hosts": {"api": "` + loginCellB + `"}}`
	loginCellBDirectory = `{"cells": {"b": {"api": "` + loginCellB + `"}}}`
	loginRegionNotice   = "→ Your organisation's data lives in another region; using " + loginCellB + "."
)

func loginAgainst(t *testing.T, issuerURL, apiURL string) (int, string, string) {
	t.Helper()
	t.Setenv("KEMORY_OIDC_ISSUER", issuerURL)
	t.Setenv("KEMORY_URL", apiURL)
	old := auth.OpenBrowser
	auth.OpenBrowser = func(string) error { return nil }
	t.Cleanup(func() { auth.OpenBrowser = old })
	return run(t, "login", "--no-browser")
}

func savedCreds(t *testing.T) *config.Credentials {
	t.Helper()
	c, err := config.LoadCredentials("prod")
	if err != nil || c == nil {
		t.Fatalf("no saved credentials: %v %v", c, err)
	}
	return c
}

func TestLoginMovesToTheOrgsHomeCellListedInTheCellDirectory(t *testing.T) {
	setHome(t)
	first := newLoginFirstAPI(t, loginCellBAnswer, loginCellBDirectory, http.StatusOK)
	routeCellB(t, cellBHandler())

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, first.URL)

	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if !strings.Contains(errOut, loginRegionNotice) {
		t.Errorf("stderr lacks the region notice: %q", errOut)
	}
	if !strings.Contains(out, "Logged in as ada@example.com · org=org-1") {
		t.Errorf("stdout lacks the login line: %q", out)
	}
	if c := savedCreds(t); c.KemoryURL != loginCellB {
		t.Errorf("saved kemory_url = %q, want %q", c.KemoryURL, loginCellB)
	}
	if first.asked("/api/v1/me") {
		t.Error("the identity lookup went to the first API, not the home cell")
	}
}

func TestLoginSavesTheHomeCellEvenWhenItCannotBeReachedAfterwards(t *testing.T) {
	setHome(t)
	first := newLoginFirstAPI(t, loginCellBAnswer, loginCellBDirectory, http.StatusOK)
	routeCellB(t, nil)

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, first.URL)

	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if !strings.Contains(errOut, loginRegionNotice) {
		t.Errorf("stderr lacks the region notice: %q", errOut)
	}
	if !strings.Contains(errOut, "could not be reached to verify the session") {
		t.Errorf("stderr lacks the unreachable warning: %q", errOut)
	}
	if c := savedCreds(t); c.KemoryURL != loginCellB {
		t.Errorf("saved kemory_url = %q, want %q", c.KemoryURL, loginCellB)
	}
}

func TestLoginStaysPutWhenTheHomeCellIsNotATrustedHost(t *testing.T) {
	setHome(t)
	// Unreachable, not guarded: a login that wrongly moves there must fail the
	// assertions below instead of panicking in the loopback guard.
	routeHost(t, "untrusted.example.test", nil)
	first := newLoginFirstAPI(t, `{"is_home_cell": false, "cell_hosts": {"api": "https://untrusted.example.test"}}`, "", http.StatusOK)

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, first.URL)

	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if strings.Contains(errOut, "lives in another region") {
		t.Errorf("login moved to an untrusted host: %q", errOut)
	}
	if !strings.Contains(out, "Logged in as") {
		t.Errorf("stdout lacks the login line: %q", out)
	}
	if c := savedCreds(t); c.KemoryURL != first.URL {
		t.Errorf("saved kemory_url = %q, want %q", c.KemoryURL, first.URL)
	}
}

func TestLoginExplainsADeviceFlowFailure(t *testing.T) {
	setHome(t)
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error": "invalid_client"}`)
	}))
	defer issuer.Close()

	code, _, errOut := loginAgainst(t, issuer.URL, "http://127.0.0.1:1")

	if code != 1 {
		t.Errorf("exit %d, want 1\nstderr: %s", code, errOut)
	}
	for _, want := range []string{
		"device_code request failed: 400",
		"Things to check:",
		"is " + issuer.URL + " reachable from this network?",
		"kemory-cli client enabled in your Keycloak realm",
		"kemory login --local",
	} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q: %q", want, errOut)
		}
	}
	if c, _ := config.LoadCredentials("prod"); c != nil {
		t.Errorf("credentials were saved after a failed login: %+v", c)
	}
}

func TestLoginExplainsAnIssuerRedirect(t *testing.T) {
	setHome(t)
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://idp.example.test/login", http.StatusFound)
	}))
	defer issuer.Close()

	code, _, errOut := loginAgainst(t, issuer.URL, "http://127.0.0.1:1")

	if code != 1 {
		t.Errorf("exit %d, want 1\nstderr: %s", code, errOut)
	}
	for _, want := range []string{"Things to check:", "is " + issuer.URL + " reachable from this network?"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q: %q", want, errOut)
		}
	}
}

func TestLoginWarnsWhenTheIdentityLookupFails(t *testing.T) {
	setHome(t)
	first := newLoginFirstAPI(t, `{"is_home_cell": true}`, "", http.StatusInternalServerError)

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, first.URL)

	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if !strings.Contains(errOut, "identity lookup failed (500)") {
		t.Errorf("stderr lacks the lookup warning: %q", errOut)
	}
	if strings.Contains(out, "Logged in as") {
		t.Errorf("stdout claims a login: %q", out)
	}
	if c := savedCreds(t); c.AccessToken != "at" || c.Email != "" {
		t.Errorf("saved access_token=%q email=%q, want \"at\" and empty", c.AccessToken, c.Email)
	}
}
