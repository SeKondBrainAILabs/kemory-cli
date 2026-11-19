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

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, first.URL)

	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	for name, s := range map[string]string{"stdout": out, "stderr": errOut} {
		if strings.Contains(s, "lives in another region") {
			t.Errorf("login moved to an unlisted host (%s): %q", name, s)
		}
	}
	if c := savedCreds(t); c.KemoryURL != first.URL {
		t.Errorf("saved kemory_url = %q, want %q", c.KemoryURL, first.URL)
	}
	if first.asked("/.well-known/kemory-cells") {
		t.Error("login asked for the cell directory")
	}
}
