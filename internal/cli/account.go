package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/releases"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/version"
)

// --- keys ------------------------------------------------------------------------

var agentLabelRE = regexp.MustCompile(`\[agent=([a-z0-9-]+)\]`)

func agentPath(id, suffix string) string {
	return "/api/v1/agents/" + url.PathEscape(id) + suffix
}

type keyResponse struct {
	AgentID string `json:"agent_id"`
	APIKey  string `json:"api_key"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// printKeyStatus warns loudly when a new key cannot authenticate yet: a
// pending agent's key 401s exactly like no credential at all.
func printKeyStatus(k keyResponse) {
	if k.Status == "" {
		return
	}
	render.Echo("  status      : " + k.Status)
	if k.Status == "active" {
		return
	}
	if k.Message != "" {
		render.Muted("  " + k.Message)
	}
	if k.Status == "pending_approval" {
		render.Warn("This key will NOT work yet — every request 401s until the agent is approved. " +
			"Approve it with: kemory keys approve " + k.AgentID)
	}
}

func newKeysCmd(g *globals) *cobra.Command {
	keys := &cobra.Command{Use: "keys", Short: "Manage org-scoped API keys.", Args: cobra.NoArgs}

	var name, label, description, reason string
	var write bool
	create := &cobra.Command{
		Use:   "create",
		Short: "Create an API key. Read-only unless --write.",
		Long: "Create an org-scoped API key for an agent. Keys are read-only by default\n" +
			"(least privilege); pass --write to allow changes. --for tags the key with\n" +
			"the agent platform it is for, so `kemory keys list` can attribute it.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if name == "" {
				return exits.New("--name is required", exits.Usage)
			}
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			desc := orDefault(description, "Issued via `kemory keys create`")
			if label != "" {
				desc = "[agent=" + label + "] " + desc
			}
			scopes := []map[string]string{{"scope": "memory:read", "reason": reason}}
			if write {
				scopes = append(scopes, map[string]string{"scope": "memory:write", "reason": reason})
			}
			resp, err := api.Post(creds, "/api/v1/agents", map[string]any{
				"agent_name": name, "agent_description": desc, "declared_scopes": scopes,
			}, api.DefaultTimeout)
			if err != nil {
				return err
			}
			if resp.Status != 200 && resp.Status != 201 {
				return api.Fail(resp, "Key creation")
			}
			var k keyResponse
			if err := resp.JSON(&k); err != nil {
				return err
			}
			render.Success("✓ Key created. Store it now — it is not shown again.")
			// The key itself is data the user asked for: printed even with --quiet.
			render.Echo("  agent_id    : " + k.AgentID)
			render.Echo("  api_key     : " + k.APIKey)
			if label != "" {
				render.Echo("  agent_label : " + label)
			}
			printKeyStatus(k)
			return nil
		},
	}
	create.Flags().StringVar(&name, "name", "", "Agent name, unique per user (required).")
	create.Flags().StringVar(&label, "for", "", "Agent platform the key is for (e.g. cursor-cloud, chatgpt).")
	create.Flags().StringVar(&description, "description", "", "Description of the agent.")
	create.Flags().BoolVar(&write, "write", false, "Grant write access.")
	create.Flags().StringVar(&reason, "reason", "agent-default", "Reason recorded with each scope (audit trail).")

	rotate := &cobra.Command{
		Use:   "rotate <agent-id>",
		Short: "Issue a new key for an agent; the old one stops working.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			resp, err := api.Post(creds, agentPath(args[0], "/rotate-key"), nil, api.DefaultTimeout)
			if err != nil {
				return err
			}
			if resp.Status != 200 && resp.Status != 201 {
				return api.Fail(resp, "Key rotation")
			}
			var k keyResponse
			if err := resp.JSON(&k); err != nil {
				return err
			}
			render.Success("✓ Key rotated. The old key is now invalid.")
			render.Echo("  agent_id : " + k.AgentID)
			render.Echo("  api_key  : " + k.APIKey)
			printKeyStatus(k)
			return nil
		},
	}

	approve := &cobra.Command{
		Use:   "approve <agent-id>",
		Short: "Approve a pending agent so its key starts working (or re-activate a suspended one).",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			resp, err := api.Post(creds, agentPath(args[0], "/approve"), nil, api.DefaultTimeout)
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				return api.Fail(resp, "Approval")
			}
			var k keyResponse
			_ = resp.JSON(&k)
			render.Success(fmt.Sprintf("✓ Agent approved — status is now %s.", orDefault(k.Status, "?")))
			return nil
		},
	}

	var yes bool
	revoke := &cobra.Command{
		Use:   "revoke <agent-id>",
		Short: "Revoke an agent's key permanently.",
		Long:  "Revoke an agent's key permanently. The agent stays in `kemory keys list` as\nrevoked; remove it afterwards with `kemory keys delete`.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			if !yes && !confirm(fmt.Sprintf("Revoke %s? Its API key stops working immediately and cannot be restored", args[0])) {
				return exits.New("Aborted.", exits.Failure)
			}
			resp, err := api.Post(creds, agentPath(args[0], "/revoke"), nil, api.DefaultTimeout)
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				return api.Fail(resp, "Revocation")
			}
			render.Success("✓ Key revoked. It no longer authenticates.")
			render.Muted("  Remove the row with: kemory keys delete " + args[0])
			return nil
		},
	}
	revoke.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")

	del := &cobra.Command{
		Use:   "delete <agent-id>",
		Short: "Delete a revoked agent for good.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			resp, err := api.Delete(creds, agentPath(args[0], ""))
			if err != nil {
				return err
			}
			if resp.Status == 400 {
				return exits.New("delete refused: the agent must be revoked first — run `kemory keys revoke "+args[0]+"`", exits.Conflict)
			}
			if resp.Status != 204 && resp.Status != 200 {
				return api.Fail(resp, "Deletion")
			}
			render.Success("✓ Agent deleted.")
			return nil
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List your API keys and the agent each is for.",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			resp, err := api.Get(creds, "/api/v1/agents")
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				return api.Fail(resp, "GET /api/v1/agents")
			}
			if render.JSONMode() {
				return emitRaw(resp.Body)
			}
			var rows []struct {
				AgentID          string `json:"agent_id"`
				AgentName        string `json:"agent_name"`
				AgentDescription string `json:"agent_description"`
				Status           string `json:"status"`
			}
			if err := resp.JSON(&rows); err != nil {
				return err
			}
			if len(rows) == 0 {
				render.Muted("(no agents)")
				return nil
			}
			table := make([][]string, 0, len(rows))
			for _, r := range rows {
				label := "-"
				if m := agentLabelRE.FindStringSubmatch(r.AgentDescription); m != nil {
					label = m[1]
				}
				table = append(table, []string{r.AgentID, orDefault(r.AgentName, "-"), label, orDefault(r.Status, "-")})
			}
			render.Table("", []string{"AGENT ID", "NAME", "FOR", "STATUS"}, table)
			return nil
		},
	}
	keys.AddCommand(create, rotate, approve, revoke, del, list)
	return keys
}

// --- telemetry -------------------------------------------------------------------

func newTelemetryCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "telemetry <on|off|status>",
		Short: "Show or change your account's product-analytics preference.",
		Long: "Show or change your account's product-analytics preference. On by default.\n" +
			"Kemory records which commands and features you use (plus CLI version and OS),\n" +
			"never their arguments or memory contents. This is the same setting as the\n" +
			"dashboard's Privacy page. A few operational events (MCP tool calls,\n" +
			"rate-limit responses) are still recorded for reliability and abuse protection.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			state := args[0]
			if err := oneOf(state, []string{"on", "off", "status"}, "state"); err != nil {
				return err
			}
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			const path = "/api/v1/user/telemetry-preference"
			var resp *api.Response
			if state == "status" {
				resp, err = api.Get(creds, path)
			} else {
				resp, err = api.Request(creds, "PUT", path, map[string]bool{"opted_in": state == "on"}, api.DefaultTimeout)
			}
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				return api.Fail(resp, "Telemetry preference")
			}
			var p struct {
				OptedIn *bool `json:"opted_in"`
			}
			_ = resp.JSON(&p)
			on := p.OptedIn == nil || *p.OptedIn
			if on {
				render.Echo("telemetry: on")
			} else {
				render.Echo("telemetry: off")
			}
			if state == "status" {
				other := "off"
				if !on {
					other = "on"
				}
				render.Muted("  change with `kemory telemetry " + other + "`")
			}
			return nil
		},
	}
}

// --- browse ----------------------------------------------------------------------

func newBrowseCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:    "browse",
		Hidden: true, // deprecated: still answers, no longer listed
		Short:  "Deprecated: read your vault in the dashboard.",
		Long:   "Deprecated. Kept so the command points at the dashboard instead of failing.",
		Args:   cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			render.Warn("`kemory browse` is deprecated and no longer opens a terminal UI.")
			if u := strings.TrimRight(g.profile.DashboardURL, "/"); u != "" {
				render.Echo("  Read your vault here:  " + u + "/memories")
				render.Muted("  Namespaces and their timelines:  " + u + "/namespaces")
			} else {
				render.Muted("  No dashboard URL is configured for this environment — set KEMORY_DASHBOARD_URL to point at yours.")
			}
			render.Echo("")
			render.Muted("From the terminal, these do not need a browser:")
			render.Muted("  kemory recall <query>     search the vault")
			render.Muted("  kemory ask <question>     an answer with its evidence")
			render.Muted("  kemory ls / show <id>     list, then read one in full")
			return nil
		},
	}
}

// --- upgrade ---------------------------------------------------------------------

// installChannel says how this binary was installed and the command that
// upgrades it. The CLI never replaces its own binary: it runs the installer
// that put it there, which already does that safely. argv is that command
// when kemory may run it, nil when it only shows it.
type installChannel struct {
	name, command string
	argv          []string
}

// runUpgrade runs an upgrade command in the terminal. Overridable in tests.
var runUpgrade = func(argv []string) error {
	c := exec.Command(argv[0], argv[1:]...) //nolint:gosec // argv is one of the fixed upgrade commands below
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, render.Out, render.Err
	return c.Run()
}

// executablePath is the running binary. Overridable in tests.
var executablePath = os.Executable

func detectChannel() installChannel {
	switch os.Getenv("KEMORY_LAUNCHER") {
	case "npx":
		return installChannel{"npx", "npx always runs the newest release; nothing to upgrade (pin a version with `npx -y kemory@<version>`).", nil}
	case "npm":
		return installChannel{"npm", "npm install -g kemory@latest", []string{"npm", "install", "-g", "kemory@latest"}}
	}
	exe, err := executablePath()
	if err != nil {
		return installChannel{"unknown", "", nil}
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	p := filepath.ToSlash(exe)
	switch {
	case strings.Contains(p, "/archive-v") && os.Getenv("UV") != "":
		return installChannel{"uvx", "uvx kemory@latest", nil}
	case strings.Contains(p, "/Cellar/kemory/") || strings.Contains(p, "/homebrew/") || strings.Contains(p, "/linuxbrew/"):
		return installChannel{"Homebrew", "brew upgrade kemory", []string{"brew", "upgrade", "kemory"}}
	case strings.Contains(p, "/uv/tools/kemory/"):
		return installChannel{"uv", "uv tool upgrade kemory", []string{"uv", "tool", "upgrade", "kemory"}}
	case strings.Contains(p, "/pipx/venvs/kemory/"):
		return installChannel{"pipx", "pipx upgrade kemory", []string{"pipx", "upgrade", "kemory"}}
	case strings.Contains(p, "/node_modules/kemory/"):
		return installChannel{"npm", "npm install -g kemory@latest", []string{"npm", "install", "-g", "kemory@latest"}}
	case strings.Contains(p, "/site-packages/") || strings.Contains(p, "/bin/python"):
		// Which pip owns this environment is not certain, so it is shown, not run.
		return installChannel{"pip", "pip install --upgrade kemory", nil}
	}
	return installChannel{"manual", "", nil}
}

const releasesPage = "https://github.com/SeKondBrainAILabs/homebrew-s9n/releases"

func newUpgradeCmd() *cobra.Command {
	var check, yes bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the CLI to the newest release.",
		Long: "Check for a newer CLI release and upgrade this install with the tool that\n" +
			"installed it: Homebrew, npm, uv or pipx, after asking (--yes skips the\n" +
			"question). For pip it prints the command; npx and uvx always run the\n" +
			"newest release. The CLI never replaces its own binary.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			latest, err := releases.Latest(ctx)
			if err != nil {
				return exits.New("could not check for releases: "+err.Error(), exits.Network)
			}
			if latest == "" {
				return exits.New("no CLI release is published yet.", exits.NotFound)
			}
			render.Echo("Current : " + version.Version)
			render.Echo("Latest  : " + latest)
			newer, ok := releases.IsNewer(latest, version.Version)
			switch {
			case ok && !newer:
				render.Echo("")
				render.Success(fmt.Sprintf("Already on the latest CLI version (%s).", version.Version))
				return nil
			case !ok:
				render.Echo("")
				render.Warn(fmt.Sprintf("note: could not compare %q with %q.", version.Version, latest))
			}
			channel := detectChannel()
			// npm resolves "latest" from a cached index that can lag a fresh
			// release, so it is asked for the version just looked up.
			if channel.name == "npm" {
				channel.command = "npm install -g kemory@" + latest
				channel.argv = []string{"npm", "install", "-g", "kemory@" + latest}
			}
			render.Echo("")
			switch {
			case check && !ok:
				render.Echo(fmt.Sprintf("Could not tell whether %s is newer than this build. Run `kemory upgrade` to install it.", latest))
			case check:
				render.Echo("A newer version is available. Run `kemory upgrade` to install it.")
			case channel.command == "":
				render.Echo(fmt.Sprintf("Download %s for %s/%s from %s/tag/cli-v%s", latest, runtime.GOOS, runtime.GOARCH, releasesPage, latest))
			case channel.argv != nil && (yes || stdinIsTerminal()):
				if !yes && !confirm(fmt.Sprintf("Installed with %s. Upgrade to %s now with `%s`?", channel.name, latest, channel.command)) {
					render.Muted("Not upgraded. Run `" + channel.command + "` when you are ready.")
					return nil
				}
				render.Muted("Running " + channel.command + " …")
				if err := runUpgrade(channel.argv); err != nil {
					return exits.New(fmt.Sprintf("%s failed (%v). Run it yourself: %s", channel.name, err, channel.command), exits.Failure)
				}
				render.Echo("")
				render.Success(fmt.Sprintf("Upgraded to %s. Check with `kemory --version`.", latest))
			default:
				render.Echo(fmt.Sprintf("Installed with %s. Upgrade with:\n\n    %s", channel.name, channel.command))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Only say whether a newer version is available.")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Upgrade without asking.")
	return cmd
}
