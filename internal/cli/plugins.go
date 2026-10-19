package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
)

// Claude Code and Grok Build have a Kemory plugin. Its hooks recall memory
// when a session starts and save the session when it ends; an MCP entry only
// makes the tools available. So for these hosts `connect` installs the plugin,
// and writes an MCP entry only when the plugin cannot be installed.

// pluginRepo is the GitHub repository of the Kemory plugin and its
// marketplace.
const pluginRepo = "SeKondBrainAILabs/kemory-plugin"

// pluginTimeout covers cloning the plugin repository.
const pluginTimeout = 3 * time.Minute

// pluginInstallers install the Kemory plugin into a host, through the host's
// own CLI.
var pluginInstallers = map[string]func() error{
	"claude-code": installClaudeCodePlugin,
	"grok":        installGrokPlugin,
}

// Variables so tests run no real tool.
var (
	lookPath = exec.LookPath
	runTool  = func(name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), pluginTimeout)
		defer cancel()
		return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // name is "claude" or "grok", arguments are fixed
	}
)

// errNoTool means the host's CLI is not on PATH.
type errNoTool string

func (e errNoTool) Error() string { return fmt.Sprintf("the `%s` command was not found", string(e)) }

func tool(name string, args ...string) error {
	if _, err := lookPath(name); err != nil {
		return errNoTool(name)
	}
	out, err := runTool(name, args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		return fmt.Errorf("`%s %s` failed: %s", name, strings.Join(args, " "), orDefault(msg, err.Error()))
	}
	return nil
}

func installClaudeCodePlugin() error {
	if _, err := lookPath("claude"); err != nil {
		return errNoTool("claude")
	}
	marketplace, err := claudeMarketplace()
	if err != nil {
		return err
	}
	if marketplace == "" {
		if err := tool("claude", "plugin", "marketplace", "add", pluginRepo); err != nil {
			return err
		}
		// The repository's marketplace.json names it "kemory".
		marketplace = "kemory"
	}
	return tool("claude", "plugin", "install", "kemory@"+marketplace, "--scope", "user")
}

// claudeMarketplace returns the name under which Claude Code knows the Kemory
// marketplace, or "" when it has not been added.
func claudeMarketplace() (string, error) {
	out, err := runTool("claude", "plugin", "marketplace", "list", "--json")
	if err != nil {
		return "", fmt.Errorf("`claude plugin marketplace list` failed: %w", err)
	}
	var markets []struct {
		Name string `json:"name"`
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal(out, &markets); err != nil {
		return "", errors.New("could not read `claude plugin marketplace list --json`")
	}
	for _, m := range markets {
		if strings.EqualFold(m.Repo, pluginRepo) {
			return m.Name, nil
		}
	}
	return "", nil
}

// tryPlugin installs the Kemory plugin when host has one and env is prod,
// which is what the plugin connects to. It reports whether the plugin is now
// installed and, when the install failed, why, for the MCP fallback to say.
func tryPlugin(env, host string) (bool, string) {
	install := pluginInstallers[host]
	if install == nil || env != config.DefaultEnv {
		return false, ""
	}
	if err := install(); err != nil {
		return false, "the Kemory plugin could not be installed (" + err.Error() + "), so the MCP entry was written instead"
	}
	return true, ""
}

// pluginInstalled is the result line for a host that got the plugin.
func pluginInstalled(host string) string {
	gets := "memory is recalled and saved automatically"
	if host == "grok" {
		// Grok ignores a session-start hook's output, so the plugin cannot
		// put recalled memory into the conversation there.
		gets = "sessions are saved automatically and Kemory's tools are available"
	}
	return "installed the Kemory plugin: " + gets + ". Restart " + host + " to load it"
}

func installGrokPlugin() error {
	// --trust skips Grok's confirmation prompt; running connect for Grok is the
	// user's request to install exactly this plugin.
	return tool("grok", "plugin", "install", pluginRepo, "--trust")
}
