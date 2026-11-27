package cli

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/residency"
)

const (
	loginCellA = "https://api.kemory.s9n.ai"
	loginCellB = "https://api.kemory.in.sekondbrain.ai"
)

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

// hostTransport serves one https host in-process (the home-cell discovery
// only accepts an https host without a port, so it cannot be a loopback
// server) and hands every other request to the previous transport, the
// loopback guard. With handler nil, the host cannot be reached.
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
	routeHost(t, "api.kemory.in.sekondbrain.ai", handler)
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
	loginCellBAnswer  = `{"is_home_cell": false, "cell_hosts": {"api": "` + loginCellB + `"}}`
	loginRegionNotice = "→ Your organisation's data lives in another region; using " + loginCellB + "."
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

func TestLoginMovesToTheOrgsAllowlistedHomeCell(t *testing.T) {
	setHome(t)
	first := newLoginFirstAPI(t, loginCellBAnswer, "", http.StatusOK)
	routeHost(t, "api.kemory.s9n.ai", first.Config.Handler)
	routeCellB(t, cellBHandler())

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, "")

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
	first := newLoginFirstAPI(t, loginCellBAnswer, "", http.StatusOK)
	routeHost(t, "api.kemory.s9n.ai", first.Config.Handler)
	routeCellB(t, nil)

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, "")

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
	routeHost(t, "api.kemory.s9n.ai", first.Config.Handler)

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, "")

	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if strings.Contains(errOut, "lives in another region") {
		t.Errorf("login moved to an untrusted host: %q", errOut)
	}
	if !strings.Contains(out, "Logged in as") {
		t.Errorf("stdout lacks the login line: %q", out)
	}
	if !first.asked("/api/v1/residency/cell") {
		t.Error("login never asked the prod host for the home cell")
	}
	if c := savedCreds(t); c.KemoryURL != loginCellA {
		t.Errorf("saved kemory_url = %q, want %q", c.KemoryURL, loginCellA)
	}
}

// An explicit KEMORY_URL is never moved, even when it is a compiled-in cell
// and the server names the other one.
func TestLoginSkipsHomeCellDiscoveryForAnExplicitAllowlistedBase(t *testing.T) {
	setHome(t)
	first := newLoginFirstAPI(t, loginCellBAnswer, "", http.StatusOK)
	routeHost(t, "api.kemory.s9n.ai", first.Config.Handler)
	// Unreachable, so a wrong move fails the assertions instead of panicking.
	routeCellB(t, nil)

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, loginCellA)

	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if first.asked("/api/v1/residency/cell") {
		t.Error("login asked an explicit KEMORY_URL base for the home cell")
	}
	if strings.Contains(errOut, "lives in another region") {
		t.Errorf("login moved away from KEMORY_URL: %q", errOut)
	}
	if c := savedCreds(t); c.KemoryURL != loginCellA {
		t.Errorf("saved kemory_url = %q, want %q (the KEMORY_URL base)", c.KemoryURL, loginCellA)
	}
}

// A profile whose base is not a compiled-in cell (a staging or self-hosted
// server) is never asked where the org lives, and never moved.
func TestLoginSkipsHomeCellDiscoveryForAnUnlistedProfileBase(t *testing.T) {
	home := setHome(t)
	first := newLoginFirstAPI(t, loginCellBAnswer, "", http.StatusOK)
	routeCellB(t, nil)
	issuer := loginIssuer(t)

	dir := filepath.Join(home, ".kemory")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	profiles := fmt.Sprintf(`{"staging": {"issuer": %q, "kemory_url": %q, "server_name": "kemory-staging"}}`, issuer.URL, first.URL)
	if err := os.WriteFile(filepath.Join(dir, "environments.json"), []byte(profiles), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEMORY_ENVIRONMENTS_FILE", "")
	t.Setenv("KEMORY_URL", "")
	t.Setenv("KEMORY_OIDC_ISSUER", issuer.URL)
	old := auth.OpenBrowser
	auth.OpenBrowser = func(string) error { return nil }
	t.Cleanup(func() { auth.OpenBrowser = old })

	code, out, errOut := run(t, "--env", "staging", "login", "--no-browser")

	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if first.asked("/api/v1/residency/cell") {
		t.Error("login asked a non-allowlisted profile base for the home cell")
	}
	if strings.Contains(errOut, "lives in another region") {
		t.Errorf("login moved away from the profile base: %q", errOut)
	}
	c, err := config.LoadCredentials("staging")
	if err != nil || c == nil {
		t.Fatalf("no saved staging credentials: %v %v", c, err)
	}
	if c.KemoryURL != first.URL {
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

// reloginCell is one fake API host for the re-login tests. It records every
// request's path and X-Organization-ID, checks the bearer token, answers
// /me through me (which sees the header), /me/orgs with a fixed status and
// body, and the residency question with loginCellBAnswer.
type reloginCell struct {
	me         func(org string) (int, string)
	orgsStatus int
	orgsBody   string

	mu   sync.Mutex
	reqs []reloginReq
}

type reloginReq struct{ path, org string }

func okMe(string) (int, string) { return http.StatusOK, meBody }

const orgsOneAndTwo = `[{"org_id": "org-1", "name": "Acme"}, {"org_id": "org-2", "name": "Beta"}]`

func (c *reloginCell) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	org := r.Header.Get("X-Organization-ID")
	c.mu.Lock()
	c.reqs = append(c.reqs, reloginReq{r.URL.Path, org})
	c.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer at" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/api/v1/residency/cell":
		fmt.Fprint(w, loginCellBAnswer)
	case "/api/v1/me":
		status, body := c.me(org)
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	case "/api/v1/me/orgs":
		w.WriteHeader(c.orgsStatus)
		fmt.Fprint(w, c.orgsBody)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// meOrgs returns the X-Organization-ID of each /me request, in order.
func (c *reloginCell) meOrgs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var orgs []string
	for _, r := range c.reqs {
		if r.path == "/api/v1/me" {
			orgs = append(orgs, r.org)
		}
	}
	return orgs
}

func (c *reloginCell) asked(path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.reqs {
		if r.path == path {
			return true
		}
	}
	return false
}

func newReloginAPI(t *testing.T, orgsStatus int, orgsBody string, me func(org string) (int, string)) (*reloginCell, *httptest.Server) {
	t.Helper()
	cell := &reloginCell{me: me, orgsStatus: orgsStatus, orgsBody: orgsBody}
	srv := httptest.NewServer(cell)
	t.Cleanup(srv.Close)
	return cell, srv
}

func saveEarlierLogin(t *testing.T, url, orgID, activeOrgID string) {
	t.Helper()
	prev := &config.Credentials{
		AccessToken: "old", ExpiresAt: 1, Issuer: "x", ClientID: "kemory-cli",
		KemoryURL: url, Env: "prod", OrgID: orgID, ActiveOrgID: activeOrgID,
	}
	if err := prev.Save(); err != nil {
		t.Fatal(err)
	}
}

func mustLogin(t *testing.T, apiURL string) (string, string) {
	t.Helper()
	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, apiURL)
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	return out, errOut
}

func wantSaved(t *testing.T, org, active string) *config.Credentials {
	t.Helper()
	c := savedCreds(t)
	if c.OrgID != org || c.ActiveOrgID != active {
		t.Errorf("saved org_id=%q active_org_id=%q, want %q and %q", c.OrgID, c.ActiveOrgID, org, active)
	}
	return c
}

func wantContains(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q: %q", what, w, got)
		}
	}
}

func TestReloginKeepsASelectedOrgTheUserStillBelongsTo(t *testing.T) {
	setHome(t)
	cell, _ := newReloginAPI(t, http.StatusOK, orgsOneAndTwo, okMe)
	routeHost(t, "api.kemory.s9n.ai", cell)
	routeCellB(t, nil)
	saveEarlierLogin(t, loginCellA, "org-1", "org-2")

	out, _ := mustLogin(t, "")

	c := wantSaved(t, "org-1", "org-2")
	if c.KemoryURL != loginCellA {
		t.Errorf("saved url %q, want %q", c.KemoryURL, loginCellA)
	}
	if cell.asked("/api/v1/residency/cell") {
		t.Error("login ran home-cell discovery")
	}
	if got := cell.meOrgs(); len(got) < 2 || got[0] != "" || got[len(got)-1] != "org-2" {
		t.Errorf("/me X-Organization-ID per call = %q, want first empty and a later org-2", got)
	}
	wantContains(t, "stdout", out, "org=org-2")
}

func TestReloginAsAnotherUserDropsTheirSelection(t *testing.T) {
	setHome(t)
	cell, srv := newReloginAPI(t, http.StatusOK, `[{"org_id": "org-1"}, {"org_id": "org-3"}]`, okMe)
	saveEarlierLogin(t, srv.URL, "org-7", "org-9")

	_, errOut := mustLogin(t, srv.URL)

	wantSaved(t, "org-1", "")
	wantContains(t, "stderr", errOut, "org-9 is not one of this sign-in's orgs", "kemory use")
	for _, org := range cell.meOrgs() {
		if org != "" {
			t.Errorf("a /me request carried X-Organization-ID %q", org)
		}
	}
}

func TestReloginDropsASelectionItCannotConfirm(t *testing.T) {
	setHome(t)
	_, srv := newReloginAPI(t, http.StatusInternalServerError, "", okMe)
	saveEarlierLogin(t, srv.URL, "org-1", "org-2")

	_, errOut := mustLogin(t, srv.URL)

	wantSaved(t, "org-1", "")
	wantContains(t, "stderr", errOut, "Could not confirm org org-2")
}

func TestReloginDoesNotSaveASelectionWhenMeFails(t *testing.T) {
	setHome(t)
	_, srv := newReloginAPI(t, http.StatusOK, orgsOneAndTwo, func(string) (int, string) {
		return http.StatusInternalServerError, ""
	})
	saveEarlierLogin(t, srv.URL, "org-1", "org-2")

	_, errOut := mustLogin(t, srv.URL)

	wantContains(t, "stderr", errOut, "identity lookup failed (500)")
	wantSaved(t, "", "")
}

func TestReloginWithNoSavedSignInOrgStillSkipsDiscovery(t *testing.T) {
	setHome(t)
	cell, _ := newReloginAPI(t, http.StatusOK, orgsOneAndTwo, okMe)
	routeHost(t, "api.kemory.s9n.ai", cell)
	routeCellB(t, nil)
	saveEarlierLogin(t, loginCellA, "", "org-2")

	mustLogin(t, "")

	if cell.asked("/api/v1/residency/cell") {
		t.Error("login ran home-cell discovery")
	}
	wantSaved(t, "org-1", "org-2")
}

func TestReloginAfterLocalLoginCarriesNoSelection(t *testing.T) {
	setHome(t)
	first := newLoginFirstAPI(t, loginCellBAnswer, "", http.StatusOK)
	routeHost(t, "api.kemory.s9n.ai", first.Config.Handler)
	routeCellB(t, cellBHandler())
	prev := &config.Credentials{
		AccessToken: "key", ExpiresAt: 1, Issuer: "local", ClientID: "local",
		KemoryURL: loginCellA, Env: "prod", ActiveOrgID: "org-2",
	}
	if err := prev.Save(); err != nil {
		t.Fatal(err)
	}

	mustLogin(t, "")

	if !first.asked("/api/v1/residency/cell") {
		t.Error("login skipped home-cell discovery")
	}
	c := wantSaved(t, "org-1", "org-1")
	if c.KemoryURL != loginCellB {
		t.Errorf("saved url %q, want %q", c.KemoryURL, loginCellB)
	}
}

func TestReloginWithTheSignInOrgSelectedStillRunsDiscovery(t *testing.T) {
	setHome(t)
	first := newLoginFirstAPI(t, loginCellBAnswer, "", http.StatusOK)
	cellB := &reloginCell{me: okMe, orgsStatus: http.StatusOK, orgsBody: orgsOneAndTwo}
	routeHost(t, "api.kemory.s9n.ai", first.Config.Handler)
	routeCellB(t, cellB)
	saveEarlierLogin(t, loginCellA, "org-1", "org-1")

	mustLogin(t, "")

	if !first.asked("/api/v1/residency/cell") {
		t.Error("login skipped home-cell discovery")
	}
	c := wantSaved(t, "org-1", "org-1")
	if c.KemoryURL != loginCellB {
		t.Errorf("saved url %q, want %q", c.KemoryURL, loginCellB)
	}
	for _, org := range cellB.meOrgs() {
		if org != "" {
			t.Errorf("a /me request carried X-Organization-ID %q", org)
		}
	}
}

func TestReloginDropsASelectionTheOrgRefuses(t *testing.T) {
	setHome(t)
	_, srv := newReloginAPI(t, http.StatusOK, orgsOneAndTwo, func(org string) (int, string) {
		if org == "org-2" {
			return http.StatusForbidden, `{"detail": "not a member"}`
		}
		return http.StatusOK, meBody
	})
	saveEarlierLogin(t, srv.URL, "org-1", "org-2")

	out, errOut := mustLogin(t, srv.URL)

	c := wantSaved(t, "org-1", "")
	if c.KemoryURL != srv.URL {
		t.Errorf("saved url %q, want %q", c.KemoryURL, srv.URL)
	}
	wantContains(t, "stderr", errOut, "Could not use org org-2 after sign-in (403)")
	wantContains(t, "stdout", out, "org=org-1")
}

// crossCells serves the zurich and mumbai hosts in-process. The sign-in org
// org-1 lives on mumbai and the selected org org-2 on zurich. zurichMe
// decides how zurich answers /me with org-2 selected.
func crossCells(t *testing.T, zurichWithOrg2 func() (int, string)) (zurich *reloginCell) {
	t.Helper()
	zurich = &reloginCell{orgsStatus: http.StatusOK, orgsBody: orgsOneAndTwo, me: func(org string) (int, string) {
		if org == "org-2" {
			return zurichWithOrg2()
		}
		return http.StatusForbidden, refusalNaming(mumbaiAPI)
	}}
	mumbai := &reloginCell{orgsStatus: http.StatusOK, orgsBody: orgsOneAndTwo, me: func(org string) (int, string) {
		if org == "org-2" {
			return http.StatusForbidden, refusalNaming(zurichAPI)
		}
		return http.StatusOK, meBody
	}}
	routeHost(t, "api.kemory.s9n.ai", zurich)
	routeHost(t, "api.kemory.in.sekondbrain.ai", mumbai)
	return zurich
}

func TestReloginFollowsTheSelectedOrgToItsCell(t *testing.T) {
	setHome(t)
	zurich := crossCells(t, func() (int, string) { return http.StatusOK, meBody })
	saveEarlierLogin(t, zurichAPI, "org-1", "org-2")

	out, _ := mustLogin(t, zurichAPI)

	c := wantSaved(t, "org-1", "org-2")
	if c.KemoryURL != zurichAPI {
		t.Errorf("saved url %q, want %q", c.KemoryURL, zurichAPI)
	}
	if zurich.asked("/api/v1/residency/cell") {
		t.Error("zurich was asked for home-cell discovery")
	}
	wantContains(t, "stdout", out, "org=org-2")
}

func TestReloginRestoresTheCellWhenTheSelectedOrgsCellRefusesToo(t *testing.T) {
	setHome(t)
	crossCells(t, func() (int, string) { return http.StatusForbidden, refusalNaming(mumbaiAPI) })
	saveEarlierLogin(t, zurichAPI, "org-1", "org-2")

	_, errOut := mustLogin(t, zurichAPI)

	c := wantSaved(t, "org-1", "")
	if c.KemoryURL != mumbaiAPI {
		t.Errorf("saved url %q, want %q", c.KemoryURL, mumbaiAPI)
	}
	wantContains(t, "stderr", errOut, "Could not use org org-2 after sign-in (its region could not be reached)")
}
