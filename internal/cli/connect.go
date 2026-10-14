package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

const hostAll = "all"

func hostChoices() string {
	return strings.Join(append(append([]string{}, hosts.Names...), hostAll), ", ")
}

func validateHosts(names []string) error {
	for _, h := range names {
		if h != hostAll && !hosts.IsKnown(h) {
			return exits.New(fmt.Sprintf("unknown --host %q (choose: %s)", h, hostChoices()), exits.Usage)
		}
	}
	return nil
}

func newMCPInstallCmd(g *globals) *cobra.Command {
	var hostNames []string
	var configPath, name string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write the kemory MCP server entry into one or more MCP hosts.",
		Long: "Write the kemory MCP server entry into MCP hosts. The entry only starts the\n" +
			"bridge; no token or key is written into the host's config, so it is safe to\n" +
			"commit or sync. `--host all` wires every supported host installed here.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if err := validateHosts(hostNames); err != nil {
				return err
			}
			return installHosts(g, hostNames, configPath, name)
		},
	}
	cmd.Flags().StringSliceVar(&hostNames, "host", []string{"claude-code"}, "MCP host(s) to configure: "+hostChoices()+".")
	cmd.Flags().StringVar(&configPath, "config", "", "Write to this config file instead of detecting the host's.")
	cmd.Flags().StringVar(&name, "name", "", "Server name under mcpServers (default: the env's, so envs don't collide).")
	return cmd
}

type hostResult struct {
	host, detail string
	ok           bool
}

func installHosts(g *globals, hostNames []string, configPath, name string) error {
	if name == "" {
		name = g.profile.ServerName
	}
	launch := hosts.LaunchFor(g.env)
	if configPath != "" {
		if err := hosts.WriteEntry(configPath, name, launch); err != nil {
			return exits.New(err.Error(), exits.Failure)
		}
		render.Success(fmt.Sprintf("✓ Wrote MCP server entry '%s' into %s", name, configPath))
		return nil
	}

	var targets []string
	var results []hostResult
	all := false
	for _, h := range hostNames {
		if h == hostAll {
			all = true
		} else if !contains(targets, h) {
			targets = append(targets, h)
		}
	}
	if all {
		// "all" means every host INSTALLED here: writing the rest would create
		// config dirs for apps the user does not have. An explicit --host
		// still writes, installed or not.
		for _, h := range hosts.Names {
			if contains(targets, h) {
				continue
			}
			if hosts.Installed(h) {
				targets = append(targets, h)
			} else {
				results = append(results, hostResult{host: h, detail: "not installed — skipped"})
			}
		}
		if len(targets) == 0 {
			return exits.New(fmt.Sprintf("No supported MCP host found on this machine (%s). Name one explicitly with --host.",
				strings.Join(hosts.Names, ", ")), exits.Failure)
		}
	}

	written := map[string]bool{}
	var lines []hostResult
	for _, h := range targets {
		path := hosts.ResolveConfig(h)
		if err := hosts.WriteEntry(path, name, launch); err != nil {
			lines = append(lines, hostResult{host: h, detail: err.Error()})
			continue
		}
		written[h] = true
		lines = append(lines, hostResult{host: h, detail: path, ok: true})
	}
	lines = append(lines, results...)
	for _, r := range lines {
		if r.ok {
			render.Success(fmt.Sprintf("✓ %-15s %s", r.host, r.detail))
		} else {
			render.Warn(fmt.Sprintf("✗ %-15s %s", r.host, r.detail))
		}
	}
	if len(written) == 0 {
		return nil
	}
	render.Echo("")
	if len(written) > 1 || !written["warp"] {
		render.Echo("Restart the affected MCP host(s) to pick up the change.")
	}
	if written["warp"] {
		render.Echo("Warp auto-detects .mcp.json on save — no restart needed.")
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func newConnectCmd(g *globals) *cobra.Command {
	var hostNames []string
	var noBrowser, local bool
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect an MCP host to Kemory in one command.",
		Long: "Sign in when needed, write the MCP server entry, and check the connection.\n" +
			"Safe to re-run: every step is idempotent.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if err := validateHosts(hostNames); err != nil {
				return err
			}
			return connect(g, hostNames, noBrowser, local)
		},
	}
	cmd.Flags().StringSliceVar(&hostNames, "host", []string{"claude-code"}, "MCP host(s) to configure: "+hostChoices()+".")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the sign-in URL but don't open it.")
	cmd.Flags().BoolVar(&local, "local", false, "Use a machine-local API key from KEMORY_API_KEY instead of signing in (dev mode).")
	return cmd
}

func connect(g *globals, hostNames []string, noBrowser, local bool) error {
	creds, err := auth.ValidCredentials(g.env, false)
	if err != nil {
		return err
	}
	if creds == nil {
		render.Echo(fmt.Sprintf("No credentials for env '%s' — starting login…", g.env))
		if local {
			err = loginLocal(g)
		} else {
			err = loginDeviceFlow(g, !noBrowser)
		}
		if err != nil {
			return err
		}
		if creds, err = auth.ValidCredentials(g.env, false); err != nil {
			return err
		}
		if creds == nil {
			return exits.New("Login did not produce credentials; aborting connect.", exits.Auth)
		}
	} else {
		render.Success(fmt.Sprintf("✓ Already logged in to '%s'", g.env))
	}

	if err := installHosts(g, hostNames, "", ""); err != nil {
		return err
	}

	// The host config is written by now, so a failed check is reported, not
	// fatal: a transient API error must not read as "start over".
	resp, err := api.Get(creds, "/api/v1/me")
	switch {
	case err != nil:
		render.Warn(fmt.Sprintf("⚠ Host config written, but the check call failed (%v). Run `kemory doctor`.", err))
	case resp.Status != 200:
		render.Warn(fmt.Sprintf("⚠ Host config written, but the check call returned %d. Run `kemory doctor` to diagnose.", resp.Status))
	default:
		var m struct {
			Email  string `json:"email"`
			UserID string `json:"user_id"`
		}
		_ = resp.JSON(&m)
		render.Success("✓ Connected as " + orDefault(firstNonEmpty(m.Email, m.UserID), "your account"))
	}
	return nil
}
