package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/residency"
)

func newLoginCmd(g *globals) *cobra.Command {
	var noBrowser, local bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to the selected environment.",
		Long: "Log in with the OAuth 2.0 device flow. --local skips OAuth and stores a\n" +
			"Kemory API key instead, for self-hosted and dev-mode setups. The URL, issuer\n" +
			"and client come from the env profile (override with KEMORY_URL,\n" +
			"KEMORY_OIDC_ISSUER and KEMORY_CLI_CLIENT_ID).",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if local {
				return loginLocal(g)
			}
			return loginDeviceFlow(g, !noBrowser)
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the URL but don't open it.")
	cmd.Flags().BoolVar(&local, "local", false, "Skip OAuth and store a machine-local API key from KEMORY_API_KEY (dev mode).")
	return cmd
}

func loginLocal(g *globals) error {
	key := os.Getenv("KEMORY_API_KEY")
	if key == "" {
		return exits.New("--local mode needs an API key. Set KEMORY_API_KEY=<key> in the environment, "+
			"then re-run `kemory login --local`.", exits.Failure)
	}
	// A pseudo-credential with a far-future expiry and a placeholder issuer,
	// so a refresh is never attempted.
	creds := &config.Credentials{
		AccessToken: key,
		ExpiresAt:   float64(time.Now().Add(365 * 24 * time.Hour).Unix()),
		Issuer:      "local",
		ClientID:    "local",
		KemoryURL:   g.profile.KemoryURL,
		Env:         g.env,
	}
	if err := creds.Save(); err != nil {
		return err
	}
	render.Success("✓ Local credential stored. The MCP bridge will forward X-API-Key to " + g.profile.KemoryURL + ".")
	return nil
}

func loginDeviceFlow(g *globals, openBrowser bool) error {
	p := g.profile
	// auth.Login overwrites the credentials file, so read the earlier
	// selection first. It is only remembered here, never put on creds before
	// the new sign-in has listed its orgs.
	var carriedActive, carriedOrg string
	if prev, perr := config.LoadCredentials(g.env); perr == nil && prev != nil && !prev.IsLocal() && prev.ActiveOrgID != "" {
		carriedActive, carriedOrg = prev.ActiveOrgID, prev.OrgID
	}
	var creds *config.Credentials
	var err error
	if g.screen != nil {
		creds, err = auth.LoginWith(p.Issuer, p.ClientID, p.KemoryURL, g.env, openBrowser, g.screen.loginUI())
	} else {
		creds, err = auth.Login(p.Issuer, p.ClientID, p.KemoryURL, g.env, openBrowser, stdoutWriter())
	}
	if err != nil {
		var flow *auth.FlowError
		var redirect *auth.RedirectError
		if errors.As(err, &flow) || errors.As(err, &redirect) {
			return exits.New(fmt.Sprintf("%v\n\nThings to check:\n"+
				"  • is %s reachable from this network?\n"+
				"  • is the kemory-cli client enabled in your Keycloak realm?\n"+
				"  • for self-hosted setups without Keycloak, try `kemory login --local`.", err, p.Issuer), exits.Failure)
		}
		return err
	}

	// Ask where the org's data lives before the first real request, so a
	// cross-cell login is corrected up front. "" means stay put; the 403
	// recovery in the API layer still covers every case. Discovery answers for
	// the token's org, so it is skipped when another org is selected. The guard
	// uses the previous login's selection and sign-in org; the selection is
	// confirmed after /me/orgs, and the confirm call below sets the selected
	// org's cell. An explicit KEMORY_URL or a base that is not a compiled-in
	// cell (staging, self-hosted) is never moved, as in the MCP bridge.
	if os.Getenv("KEMORY_URL") == "" && residency.IsAllowedAPIURL(creds.KemoryURL) && !residency.SelectionOverridesDiscovery(carriedActive, carriedOrg) {
		if home := residency.DiscoverHomeCell(creds.KemoryURL, api.Headers(creds)); home != "" {
			creds.KemoryURL = home
			if err := creds.Save(); err != nil {
				return err
			}
			render.Accent("→ Your organisation's data lives in another region; using " + home + ".")
		}
	}

	resp, err := api.Get(creds, "/api/v1/me")
	if err != nil {
		// Only "could not reach the host" is survivable: the tokens are cached,
		// only the verification failed.
		if exits.Code(err) != exits.Network {
			return err
		}
		render.Warn("⚠ Logged in and tokens cached, but kemory could not be reached to verify the session. Try `kemory whoami`.")
		return nil
	}
	if resp.Status != 200 {
		render.Warn(fmt.Sprintf("⚠ Logged in and tokens cached, but identity lookup failed (%d). Try `kemory whoami`.", resp.Status))
		return nil
	}
	var me struct {
		Email string `json:"email"`
		OrgID string `json:"org_id"`
		Teams []struct {
			Name string `json:"name"`
		} `json:"teams"`
	}
	_ = resp.JSON(&me)
	creds.Email = me.Email
	creds.OrgID = me.OrgID

	// Select the org automatically when the user belongs to exactly one;
	// otherwise leave the choice to `kemory use <org>`.
	orgs := fetchOrgs(creds)
	if carriedActive != "" {
		switch {
		case hasOrg(orgs, carriedActive):
			creds.ActiveOrgID = carriedActive
		case orgs != nil:
			render.Warn(fmt.Sprintf("⚠ Org %s is not one of this sign-in's orgs; selection cleared. Run `kemory use <org>` to pick one.", carriedActive))
		default:
			render.Warn(fmt.Sprintf("⚠ Could not confirm org %s for this sign-in; selection cleared. Run `kemory use <org>` to pick it again.", carriedActive))
		}
	}
	if len(orgs) == 1 {
		creds.ActiveOrgID = orgs[0].OrgID
	}
	if carriedActive != "" && creds.ActiveOrgID == carriedActive && carriedActive != creds.OrgID {
		confirmSelectedOrg(creds, carriedActive)
	}
	if err := creds.Save(); err != nil {
		return err
	}
	if g.screen != nil {
		// The guided screen says who signed in itself.
		return nil
	}
	teams := make([]string, 0, len(me.Teams))
	for _, t := range me.Teams {
		teams = append(teams, "'"+t.Name+"'")
	}
	render.Success(fmt.Sprintf("✓ Logged in as %s · org=%s · teams=[%s]",
		orDefault(creds.Email, "unknown"), orDefault(firstNonEmpty(creds.ActiveOrgID, creds.OrgID), "?"), joinComma(teams)))
	if len(orgs) > 1 {
		render.Echo(fmt.Sprintf("  You belong to %d orgs — run `kemory orgs` to list, `kemory use <org_id>` to switch.", len(orgs)))
	}
	return nil
}

// confirmSelectedOrg sends one /me with the selected org, so the API layer's
// residency recovery moves creds to that org's cell. When the org refuses or
// no cell answers, the selection is cleared and the cell from before the call
// is restored: the recovery may already have saved the new URL.
func confirmSelectedOrg(creds *config.Credentials, org string) {
	urlBefore := creds.KemoryURL
	resp, err := api.Get(creds, "/api/v1/me")
	if err == nil && resp.Status == 200 {
		return
	}
	reason := "its region could not be reached"
	if err == nil {
		reason = fmt.Sprint(resp.Status)
	}
	creds.ActiveOrgID = ""
	creds.KemoryURL = urlBefore
	render.Warn(fmt.Sprintf("⚠ Could not use org %s after sign-in (%s); selection cleared. Run `kemory use <org>` to pick it again.", org, reason))
}

func hasOrg(orgs []orgMembership, id string) bool {
	for _, o := range orgs {
		if o.OrgID == id {
			return true
		}
	}
	return false
}

type orgMembership struct {
	OrgID string `json:"org_id"`
	Name  string `json:"name"`
}

// fetchOrgs lists the user's org memberships, or nil when the lookup fails.
func fetchOrgs(creds *config.Credentials) []orgMembership {
	resp, err := api.Get(creds, "/api/v1/me/orgs")
	if err != nil || resp.Status != 200 {
		return nil
	}
	var orgs []orgMembership
	if json.Unmarshal(resp.Body, &orgs) != nil {
		return nil
	}
	return orgs
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func joinComma(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
