package cli

import (
	"net/http"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
)

// A re-login while another org is selected must not act on home-cell
// discovery, which answers for the token's org (login.go's
// SelectionOverridesDiscovery guard).
func TestReloginWithAnotherOrgSelectedSkipsHomeCellDiscovery(t *testing.T) {
	setHome(t)
	first := newLoginFirstAPI(t, loginCellBAnswer, "", http.StatusOK)
	routeCellB(t, cellBHandler())

	prev := &config.Credentials{
		AccessToken: "old", ExpiresAt: 1, Issuer: "x", ClientID: "kemory-cli",
		KemoryURL: first.URL, Env: "prod",
		OrgID: "org-1", ActiveOrgID: "org-2",
	}
	if err := prev.Save(); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := loginAgainst(t, loginIssuer(t).URL, first.URL)
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
	if first.asked("/api/v1/residency/cell") {
		t.Errorf("login ran home-cell discovery although org-2 was selected (token org org-1); stderr: %q", errOut)
	}
}
