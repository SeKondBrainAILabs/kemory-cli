package cli

import (
	"net/http"
	"strings"
	"testing"
)

// A login against a base that is not a compiled-in cell, given explicitly with
// KEMORY_URL, must not ask that server where the org lives nor move to the
// host it names, as the MCP bridge's ensureHomeCell does not.
func TestLoginSkipsHomeCellDiscoveryForAnExplicitUnlistedBase(t *testing.T) {
	setHome(t)
	first := newLoginFirstAPI(t, loginCellBAnswer, "", http.StatusOK)
	routeCellB(t, cellBHandler())

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, first.URL)

	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if first.asked("/api/v1/residency/cell") {
		t.Error("login asked a non-allowlisted KEMORY_URL base for the home cell")
	}
	if strings.Contains(errOut, "lives in another region") {
		t.Errorf("login moved away from KEMORY_URL: %q", errOut)
	}
	if c := savedCreds(t); c.KemoryURL != first.URL {
		t.Errorf("saved kemory_url = %q, want %q (the KEMORY_URL base)", c.KemoryURL, first.URL)
	}
}
