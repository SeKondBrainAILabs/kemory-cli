package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

func newLogoutCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Delete the cached credentials for the selected environment.",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			path, err := config.CredentialsPath(g.env)
			if err != nil {
				return err
			}
			err = os.Remove(path)
			if errors.Is(err, fs.ErrNotExist) {
				render.Echo(fmt.Sprintf("(no credentials file present for env '%s')", g.env))
				return nil
			}
			if err != nil {
				return err
			}
			render.Echo("✓ Removed " + path)
			return nil
		},
	}
}

type me struct {
	Email   string   `json:"email"`
	UserID  string   `json:"user_id"`
	OrgID   string   `json:"org_id"`
	OrgName string   `json:"org_name"`
	OrgRole string   `json:"org_role"`
	Roles   []string `json:"roles"`
	Teams   []struct {
		Name     string `json:"name"`
		Role     string `json:"role"`
		CanWrite bool   `json:"can_write"`
	} `json:"teams"`
}

func newWhoamiCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Print identity, org, and team membership.",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			resp, err := api.Get(creds, "/api/v1/me")
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				return exits.New(fmt.Sprintf("GET /api/v1/me failed: %d %s", resp.Status, resp.Body), exits.Failure)
			}
			if render.JSONMode() {
				// Indent without decoding, so the server's key order survives.
				var out bytes.Buffer
				if err := json.Indent(&out, resp.Body, "", "  "); err != nil {
					return err
				}
				render.JSON(out.Bytes())
				return nil
			}
			var m me
			if err := resp.JSON(&m); err != nil {
				return err
			}
			orgName := m.OrgName
			// Older servers hard-code org_name to the org id; resolve the human
			// name from /me/orgs, and never print the same id twice.
			if orgName == m.OrgID {
				orgName = resolveOrgName(creds, m.OrgID)
			}
			orgLine := m.OrgID
			if orgName != "" && orgName != m.OrgID {
				orgLine = fmt.Sprintf("%s (%s)", orgName, m.OrgID)
			}
			if m.OrgRole != "" {
				orgLine += "  role=" + m.OrgRole
			}
			roles := strings.Join(m.Roles, ", ")
			if roles == "" {
				roles = "-"
			}
			render.KVs([]render.KV{
				{Key: "Email", Value: m.Email},
				{Key: "User ID", Value: m.UserID},
				{Key: "Active org", Value: orgLine},
				{Key: "Roles", Value: roles},
			})
			if creds.ActiveOrgID != "" {
				render.Muted("  ↳ selected locally via `kemory use " + creds.ActiveOrgID + "`")
			} else {
				render.Muted("  ↳ using token default — run `kemory orgs` to see switchable orgs")
			}
			render.Echo("")
			if len(m.Teams) == 0 {
				render.Muted("Teams: (none)")
				return nil
			}
			rows := make([][]string, 0, len(m.Teams))
			for _, t := range m.Teams {
				canWrite := "no"
				if t.CanWrite {
					canWrite = "yes"
				}
				rows = append(rows, []string{t.Name, t.Role, canWrite})
			}
			render.Table("Teams", []string{"TEAM", "ROLE", "CAN WRITE"}, rows)
			return nil
		},
	}
}

// resolveOrgName returns the human name of orgID from /me/orgs, or "".
// Cosmetic: whoami never fails over it.
func resolveOrgName(creds *config.Credentials, orgID string) string {
	if orgID == "" {
		return ""
	}
	for _, o := range fetchOrgs(creds) {
		if o.OrgID == orgID && o.Name != orgID {
			return o.Name
		}
	}
	return ""
}

// stdoutWriter is where interactive login prompts go: stdout, or stderr in
// JSON mode.
func stdoutWriter() io.Writer {
	if render.JSONMode() {
		return render.Err
	}
	return render.Out
}
