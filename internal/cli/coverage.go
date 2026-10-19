package cli

import (
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
)

// A host can reach Kemory without an entry kemory writes: Claude Code and Grok
// through the Kemory plugin, Claude Desktop through the claude.ai connector. A local
// entry on top of either gives the host every Kemory tool twice.

// connectorFreshness is how recently a claude.ai connector must have been
// used to count. Reconnecting leaves the old connection behind, active but
// idle, so an old one says nothing about today.
const connectorFreshness = 30 * 24 * time.Hour

// coveredElsewhere maps a host to the way it already reaches Kemory in env.
// creds may be nil, and an API error only means the connector is not counted.
func coveredElsewhere(env string, creds *config.Credentials, now time.Time) map[string]string {
	covered := map[string]string{}
	// The plugin connects to prod; another env still needs its own entry.
	if path := hosts.ClaudeCodePluginSettings(); path != "" && env == config.DefaultEnv {
		covered["claude-code"] = "connected through the Kemory plugin (" + path + ")"
	}
	if hosts.GrokPluginConfig() != "" && env == config.DefaultEnv {
		covered["grok"] = "connected through the Kemory plugin (`grok plugin list`)"
	}
	if creds != nil && hasClaudeAIConnector(creds, now) {
		covered["claude-desktop"] = "connected through the claude.ai connector"
	}
	return covered
}

// hasClaudeAIConnector reports whether the account has a claude.ai connector
// in use. claude.ai connectors also run in Claude Desktop, signed in with the
// same Claude account.
func hasClaudeAIConnector(creds *config.Credentials, now time.Time) bool {
	resp, err := api.Get(creds, "/api/v1/agents")
	if err != nil || resp.Status != 200 {
		return false
	}
	var agents []struct {
		Status       string `json:"status"`
		Kind         string `json:"agent_kind"`
		ClientSlug   string `json:"client_slug"`
		LastActiveAt string `json:"last_active_at"`
	}
	if resp.JSON(&agents) != nil {
		return false
	}
	for _, a := range agents {
		if a.Status != "active" || a.Kind != "oauth" || a.ClientSlug != "claude-web" {
			continue
		}
		if at, err := time.Parse(time.RFC3339Nano, a.LastActiveAt); err == nil && now.Sub(at) < connectorFreshness {
			return true
		}
	}
	return false
}

// named keeps the entries called server, the one this environment writes.
func named(entries []string, server string) []string {
	var out []string
	for _, e := range entries {
		if e == server {
			out = append(out, e)
		}
	}
	return out
}
