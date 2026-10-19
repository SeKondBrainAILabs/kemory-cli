package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var orgTypeLabel = map[string]string{"personal": "personal", "organisation": "org", "family": "family"}

type orgListing struct {
	OrgID   string `json:"org_id"`
	Name    string `json:"name"`
	OrgType string `json:"org_type"`
	Role    string `json:"role"`
	Active  bool   `json:"active"`
}

func newOrgsCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "orgs",
		Short: "List the organisations you can switch between, marking the active one.",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			var resp *api.Response
			loading("Loading your organisations…", func() { resp, err = api.Get(creds, "/api/v1/me/orgs") })
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				return api.Fail(resp, "GET /api/v1/me/orgs")
			}
			if render.JSONMode() {
				var out bytes.Buffer
				if err := json.Indent(&out, resp.Body, "", "  "); err != nil {
					return err
				}
				render.JSON(out.Bytes())
				return nil
			}
			var orgs []orgListing
			if err := resp.JSON(&orgs); err != nil {
				return err
			}
			if len(orgs) == 0 {
				render.Muted("(no org memberships found)")
				return nil
			}
			sort.SliceStable(orgs, func(i, j int) bool {
				if orgs[i].Active != orgs[j].Active {
					return orgs[i].Active
				}
				return strings.ToLower(orgs[i].Name) < strings.ToLower(orgs[j].Name)
			})
			rows := make([][]string, 0, len(orgs))
			for _, o := range orgs {
				marker := "○"
				if o.Active {
					marker = "●"
				}
				label, ok := orgTypeLabel[o.OrgType]
				if !ok {
					label = "-"
				}
				rows = append(rows, []string{marker, orDefault(o.Name, o.OrgID), label, o.OrgID, orDefault(o.Role, "-")})
			}
			render.Table("", []string{"", "NAME", "TYPE", "ORG ID", "ROLE"}, rows)
			render.Muted("Switch:  kemory use <name|org_id>")
			return nil
		},
	}
}

func newUseCmd(g *globals) *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "use [org]",
		Short: "Switch the active organisation.",
		Long: "Switch the active organisation. <org> may be a full org id, an id prefix or\n" +
			"the org name (case-insensitive). The choice is saved and sent with every\n" +
			"request; the server checks membership each time. No re-login.\n\n" +
			"--clear drops the selection so the organisation on your sign-in applies again.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			org := ""
			if len(args) == 1 {
				org = strings.TrimSpace(args[0])
			}
			return useOrg(g, org, clear)
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "Drop the selection and use the organisation on your sign-in.")
	return cmd
}

func useOrg(g *globals, org string, clear bool) error {
	creds, err := api.RequireCreds(g.env)
	if err != nil {
		return err
	}
	if clear {
		if org != "" {
			return exits.New("pass either an org or --clear, not both.", exits.Usage)
		}
		if creds.ActiveOrgID == "" {
			render.Echo("No active organisation was selected — nothing to clear.")
			return nil
		}
		previous := creds.ActiveOrgID
		creds.ActiveOrgID = ""
		if err := creds.Save(); err != nil {
			return err
		}
		render.Success(fmt.Sprintf("✓ Cleared the selected organisation (%s). Requests now use the organisation on your sign-in.", previous))
		return nil
	}
	if org == "" {
		return exits.New("give an org (id, id prefix, or name), or --clear.", exits.Usage)
	}

	target, matchedName := org, ""
	if !uuidRE.MatchString(org) {
		resp, err := api.Get(creds, "/api/v1/me/orgs")
		if err != nil {
			return err
		}
		if resp.Status != 200 {
			return exits.New(fmt.Sprintf("could not list orgs to resolve '%s': %d %s", org, resp.Status, resp.Body), exits.ForStatus(resp.Status))
		}
		var orgs []orgListing
		if err := resp.JSON(&orgs); err != nil {
			return err
		}
		var matches []orgListing
		for _, o := range orgs {
			if strings.EqualFold(o.Name, org) || strings.HasPrefix(o.OrgID, org) {
				matches = append(matches, o)
			}
		}
		switch len(matches) {
		case 0:
			names := make([]string, 0, len(orgs))
			for _, o := range orgs {
				names = append(names, orDefault(o.Name, o.OrgID))
			}
			return exits.New(fmt.Sprintf("no org matches '%s'. You belong to: %s", org, orDefault(strings.Join(names, ", "), "(none)")), exits.NotFound)
		case 1:
			target, matchedName = matches[0].OrgID, matches[0].Name
		default:
			return exits.New(fmt.Sprintf("'%s' is ambiguous (%d matches) — use the full org id.", org, len(matches)), exits.Usage)
		}
	}
	if !uuidRE.MatchString(target) {
		return exits.New(fmt.Sprintf("resolved id '%s' is not a valid UUID.", target), exits.Failure)
	}

	previous := creds.ActiveOrgID
	creds.ActiveOrgID = target
	if err := creds.Save(); err != nil {
		return err
	}
	// Confirm membership in the new scope. A cross-cell org is followed by the
	// API layer's residency retry. If confirmation fails, restore the previous
	// selection: a file pointed at an org this CLI cannot reach would break
	// every later command.
	revert := func() {
		creds.ActiveOrgID = previous
		_ = creds.Save()
	}
	resp, err := api.Get(creds, "/api/v1/me")
	if err != nil {
		revert()
		return err
	}
	if resp.Status == 401 || resp.Status == 403 {
		revert()
		return exits.New(fmt.Sprintf("Not a member of org %s (access denied) — active org unchanged.", target), exits.Forbidden)
	}
	if resp.Status != 200 {
		revert()
		return exits.New(fmt.Sprintf("switch verification failed: %d %s — active org unchanged.", resp.Status, resp.Body), exits.ForStatus(resp.Status))
	}
	var m struct {
		OrgName string `json:"org_name"`
		OrgRole string `json:"org_role"`
	}
	_ = resp.JSON(&m)
	render.Success(fmt.Sprintf("✓ Active org → %s (%s)  role=%s", firstNonEmpty(matchedName, m.OrgName, target), target, orDefault(m.OrgRole, "?")))
	return nil
}
