package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

// Copilot's cloud agent takes no sign-in: it reaches Kemory with an API key,
// kept in the repository's Agents secrets. KEMORY_API_KEY is for the hooks;
// the MCP configuration sees only secrets named COPILOT_MCP_*.
const (
	cloudHookSecret = "KEMORY_API_KEY"             //nolint:gosec // a secret's name, not its value
	cloudMCPSecret  = "COPILOT_MCP_KEMORY_API_KEY" //nolint:gosec // as above
)

// gitOutput runs git in dir and returns its trimmed output.
var gitOutput = func(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output() //nolint:gosec // git with arguments, no shell
	return strings.TrimSpace(string(out)), err
}

// setAgentsSecret stores value as the Agents secret name in repo, through gh.
var setAgentsSecret = func(repo, name, value string) error {
	if _, err := lookPath("gh"); err != nil {
		return err
	}
	cmd := exec.Command("gh", "secret", "set", name, "--app", "agents", "--repo", repo) //nolint:gosec // gh with arguments, no shell; the value goes on stdin
	cmd.Stdin = strings.NewReader(value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

var githubRemoteRE = regexp.MustCompile(`github\.com[:/]([^/]+/[^/]+?)(\.git)?/?$`)

func newConnectCopilotCloudCmd(g *globals) *cobra.Command {
	var repo, dir string
	cmd := &cobra.Command{
		Use:   "copilot-cloud",
		Short: "Connect GitHub Copilot's cloud agent in this repository to Kemory.",
		Long: "Give Copilot's cloud agent automatic memory in this repository: it writes\n" +
			".github/hooks/kemory.json, creates an API key for the repository and stores\n" +
			"it as the repository's Agents secrets " + cloudHookSecret + " and " + cloudMCPSecret + "\n" +
			"(through gh), then prints the MCP configuration to paste in the repository's\n" +
			"settings. Safe to re-run: it rotates the repository's key.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return connectCopilotCloud(g, dir, repo)
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository as owner/name (default: the origin remote).")
	cmd.Flags().StringVar(&dir, "dir", ".", "The repository's working tree.")
	return cmd
}

func connectCopilotCloud(g *globals, dir, repo string) error {
	root, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return exits.New("not in a git repository: run it in the repository Copilot works on, or pass --dir", exits.Usage)
	}
	if repo == "" {
		remote, _ := gitOutput(root, "remote", "get-url", "origin")
		m := githubRemoteRE.FindStringSubmatch(remote)
		if m == nil {
			return exits.New("the origin remote is not on GitHub: pass --repo owner/name", exits.Usage)
		}
		repo = m[1]
	}
	creds, err := api.RequireCreds(g.env)
	if err != nil {
		return err
	}

	path, err := hosts.InstallCopilotCloudHooks(root, g.env)
	if err != nil {
		return err
	}
	render.Success("✓ Hooks written to " + path + ". Commit and push it.")

	key, err := repoKey(creds, "copilot-cloud "+repo, repo)
	if err != nil {
		return err
	}
	var failed error
	for _, name := range []string{cloudHookSecret, cloudMCPSecret} {
		if failed = setAgentsSecret(repo, name, key); failed != nil {
			break
		}
	}
	if failed == nil {
		render.Success("✓ API key stored as the Agents secrets " + cloudHookSecret + " and " + cloudMCPSecret + " in " + repo + ".")
	} else {
		render.Warn("Could not store the key with gh (" + failed.Error() + ").")
		render.Echo("  Add it yourself in " + repo + ": Settings → Secrets and variables → Agents,")
		render.Echo("  as both " + cloudHookSecret + " and " + cloudMCPSecret + ". It is not shown again:")
		// The key itself is data the user needs: printed even with --quiet.
		render.Echo("  api_key : " + key)
	}

	host := config.EnvProfile(g.env).KemoryURL
	mcp, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{
		// A struct, so the entry reads type, url, headers, tools.
		config.EnvProfile(g.env).ServerName: struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Tools   []string          `json:"tools"`
		}{"http", strings.TrimRight(host, "/") + "/mcp/v1", map[string]string{"X-API-Key": "$" + cloudMCPSecret}, []string{"*"}},
	}}, "", "  ")
	render.Echo("")
	render.Echo("Two steps in " + repo + "'s Settings → Copilot:")
	render.Echo("  1. MCP servers: paste this as the MCP configuration and save it.")
	for _, line := range strings.Split(string(mcp), "\n") {
		render.Echo("     " + line)
	}
	if u, err := url.Parse(host); err == nil && u.Host != "" {
		render.Echo("  2. Internet access: add " + u.Host + " to the custom allowlist, so the hooks reach Kemory.")
	}
	return nil
}

// repoKey returns a new API key for the agent name: a new agent, or a new key
// for the one an earlier run made. Hooks save sessions, so it may write.
func repoKey(creds *config.Credentials, name, repo string) (string, error) {
	resp, err := api.Get(creds, "/api/v1/agents")
	if err != nil {
		return "", err
	}
	if resp.Status != 200 {
		return "", api.Fail(resp, "GET /api/v1/agents")
	}
	var agents []struct {
		AgentID   string `json:"agent_id"`
		AgentName string `json:"agent_name"`
	}
	if err := resp.JSON(&agents); err != nil {
		return "", err
	}
	id := ""
	for _, a := range agents {
		if a.AgentName == name {
			id = a.AgentID
		}
	}
	if id != "" {
		resp, err = api.Post(creds, agentPath(id, "/rotate-key"), nil, api.DefaultTimeout)
	} else {
		resp, err = api.Post(creds, "/api/v1/agents", map[string]any{
			"agent_name":        name,
			"agent_description": "[agent=copilot-cloud] Copilot cloud agent in " + repo,
			"declared_scopes": []map[string]string{
				{"scope": "memory:read", "reason": "copilot-cloud"},
				{"scope": "memory:write", "reason": "copilot-cloud"},
			},
		}, api.DefaultTimeout)
	}
	if err != nil {
		return "", err
	}
	if resp.Status != 200 && resp.Status != 201 {
		return "", api.Fail(resp, "Key creation")
	}
	var k keyResponse
	if err := resp.JSON(&k); err != nil {
		return "", err
	}
	if k.Status == "pending_approval" {
		// The user made the key, so they approve it.
		resp, err := api.Post(creds, agentPath(k.AgentID, "/approve"), nil, api.DefaultTimeout)
		if err != nil {
			return "", err
		}
		if resp.Status != 200 {
			return "", api.Fail(resp, "Approval")
		}
	}
	return k.APIKey, nil
}
