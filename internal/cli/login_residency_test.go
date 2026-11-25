package cli

import (
	"net/http"
	"strings"
	"testing"
)

// A host the server names, and also lists in its cell directory, is still not a
// compiled-in cell: login keeps its own API and never sends the token there.
func TestLoginKeepsItsHostWhenDiscoveryNamesAnUnlistedHost(t *testing.T) {
	setHome(t)
	// Unreachable, not guarded, so a login that wrongly moves there fails the
	// assertions below instead of panicking in the loopback guard. .invalid
	// can never resolve.
	routeHost(t, "new.invalid", nil)
	first := newLoginFirstAPI(t,
		`{"is_home_cell": false, "cell_hosts": {"api": "https://new.invalid"}}`,
		`{"cells": {"new": {"api": "https://new.invalid"}}}`,
		http.StatusOK)
	routeHost(t, "api.kemory.s9n.ai", first.Config.Handler)

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, "")

	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	for name, s := range map[string]string{"stdout": out, "stderr": errOut} {
		if strings.Contains(s, "lives in another region") {
			t.Errorf("login moved to an unlisted host (%s): %q", name, s)
		}
	}
	if !first.asked("/api/v1/residency/cell") {
		t.Error("login never asked the prod host for the home cell")
	}
	if c := savedCreds(t); c.KemoryURL != loginCellA {
		t.Errorf("saved kemory_url = %q, want %q", c.KemoryURL, loginCellA)
	}
	if first.asked("/.well-known/kemory-cells") {
		t.Error("login asked for the cell directory")
	}
}
