package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/ui"
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
	var dryRun bool
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
			// Without a sign-in the claude.ai connector cannot be looked up,
			// which only means it is not counted.
			creds, _ := auth.ValidCredentials(g.env, false)
			return installHosts(g, creds, hostNames, configPath, name, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would change for each host, and change nothing.")
	cmd.Flags().StringSliceVar(&hostNames, "host", []string{"claude-code"}, "MCP host(s) to configure: "+hostChoices()+".")
	cmd.Flags().StringVar(&configPath, "config", "", "Write to this config file, in the --host's format, instead of the host's own.")
	cmd.Flags().StringVar(&name, "name", "", "Server name in the host's config (default: the env's, so envs don't collide).")
	return cmd
}

type hostResult struct {
	host, detail string
	ok           bool
	// short is the detail for the guided screen; next is the step the user
	// still has to take, if any, and restart asks them to restart the host.
	short, next string
	restart     bool
}

func installHosts(g *globals, creds *config.Credentials, hostNames []string, configPath, name string, dryRun bool) error {
	if name == "" {
		name = g.profile.ServerName
	}
	launch := hosts.LaunchFor(g.env)
	if configPath != "" {
		if len(hostNames) != 1 || hostNames[0] == hostAll {
			return exits.New("--config takes exactly one --host, whose config format it uses", exits.Usage)
		}
		if dryRun {
			render.Success(fmt.Sprintf("→ would write MCP server entry '%s' into %s", name, configPath))
			return nil
		}
		if err := hosts.WriteEntry(hostNames[0], configPath, name, launch); err != nil {
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

	covered := coveredElsewhere(g.env, creds, time.Now())
	written := map[string]bool{}
	var lines []hostResult
	for _, h := range targets {
		if dryRun {
			lines = append(lines, planned(g.env, h, name, covered))
			continue
		}
		if reason := covered[h]; reason != "" && toolsOnlyPlugins[h] {
			var r hostResult
			g.screen.spin(h, func() { r = hooksBesidePlugin(h, reason, name, launch, written) })
			lines = append(lines, r)
			continue
		}
		// A second entry would give the host every Kemory tool twice.
		if reason := covered[h]; reason != "" {
			r := hostResult{
				host: h, detail: "already " + reason + " — nothing written", ok: true,
				short: "already " + reason,
			}
			if len(named(hosts.KemoryEntries(h, hosts.ResolveConfig(h)), name)) > 0 || leftoverHooks(h) {
				r.detail += "; an earlier connect left a duplicate, which `kemory doctor --fix` removes"
				r.next = "Run `kemory doctor --fix` to remove the duplicate Kemory entry"
			}
			lines = append(lines, r)
			continue
		}
		var r hostResult
		g.screen.spin(h, func() { r = connectHost(g, h, name, launch, written) })
		lines = append(lines, r)
	}
	lines = append(lines, results...)
	if g.screen != nil && !dryRun {
		g.screen.results(lines)
		return nil
	}
	for _, r := range lines {
		switch {
		case dryRun && r.ok:
			render.Echo(fmt.Sprintf("→ %-15s %s", r.host, r.detail))
		case r.ok:
			render.Success(fmt.Sprintf("✓ %-15s %s", r.host, r.detail))
		default:
			render.Warn(fmt.Sprintf("✗ %-15s %s", r.host, r.detail))
		}
	}
	if dryRun {
		render.Echo("")
		render.Muted("Dry run: nothing was changed.")
		return nil
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

// joinNote appends note to detail when there is one.
func joinNote(detail, note string) string {
	if note == "" {
		return detail
	}
	return detail + " — " + note
}

// connectHost connects one host that has no Kemory yet: the plugin where
// there is one, else the MCP entry, plus memory hooks where the host takes
// them. It records written hosts in written.
func connectHost(g *globals, h, name string, launch hosts.Launch, written map[string]bool) hostResult {
	plugin, pluginErr := tryPlugin(g.env, h)
	if plugin {
		written[h] = true
		return hostResult{
			host: h, detail: pluginInstalled(h), ok: true,
			short: level(h, "plugin"), next: "Restart " + ui.AppName(h) + " to load the Kemory plugin",
		}
	}
	path := hosts.ResolveConfig(h)
	resume := resumeArgs(g.env, "mcp", "install", "--host", h)
	done, note, err := withAppClosed(h, false, resume, func() error { return hosts.WriteEntry(h, path, name, launch) })
	switch {
	case err != nil:
		return hostResult{host: h, detail: err.Error(), short: err.Error()}
	case !done:
		return hostResult{host: h, detail: note, short: note}
	}
	written[h] = true
	r := hostResult{host: h, detail: joinNote(path, note), ok: true, short: levelTools}
	// Warp picks up its config on save.
	r.restart = h != "warp"
	if pluginErr != nil {
		r.detail = joinNote(r.detail, pluginFallback(pluginErr))
		r.short += " (no plugin: " + pluginErr.Error() + ")"
	}
	hooks, err := addHooks(h, launch)
	switch {
	case err != nil:
		r.detail = joinNote(r.detail, "memory hooks not added: "+err.Error())
		r.short += " · could not add full memory"
	case hooks != "":
		r.detail = joinNote(r.detail, hooks)
		r.short = strings.Replace(r.short, levelTools, levelFull, 1)
		if memoryHooks[h].approve != "" {
			r.restart = false
			r.next = "Open " + ui.AppName(h) + " and approve the memory hooks with /hooks"
		}
	}
	return r
}

// hooksBesidePlugin connects a host whose Kemory plugin brings the tools but
// no hooks: it adds the memory hooks, and removes a kemory entry that would
// give the host every tool twice. It records written hosts in written.
func hooksBesidePlugin(h, reason, server string, launch hosts.Launch, written map[string]bool) hostResult {
	path := hosts.ResolveConfig(h)
	dupes, needed := besidePlugin(h, path, server)
	r := hostResult{host: h, detail: "already " + reason, ok: true, short: level(h, reason) + " · Kemory plugin"}
	if len(dupes) > 0 {
		if err := hosts.RemoveEntries(h, path, dupes); err != nil {
			return hostResult{host: h, detail: err.Error(), short: err.Error()}
		}
		written[h] = true
		r.restart = true
		r.detail = joinNote(r.detail, "removed the kemory entry in "+path+", which listed every tool twice")
	}
	if !needed {
		if len(dupes) == 0 {
			r.detail = joinNote(r.detail, "automatic memory hooks already in "+memoryHooks[h].path()+" — nothing written")
		}
		return r
	}
	hooks, err := addHooks(h, launch)
	if err != nil {
		r.detail = joinNote(r.detail, "memory hooks not added: "+err.Error())
		r.short = levelTools + " · Kemory plugin · could not add full memory"
		return r
	}
	written[h] = true
	r.detail = joinNote(r.detail, hooks)
	r.short = levelFull + " · Kemory plugin"
	r.restart = false
	r.next = "Open " + ui.AppName(h) + " and approve the memory hooks with /hooks"
	return r
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
	var noBrowser, local, dryRun bool
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect your AI clients to Kemory in one command.",
		Long: "Sign in when needed, connect your AI clients, and check the connection.\n" +
			"In a terminal without --host it lists the clients it finds and asks which\n" +
			"to connect. Safe to re-run: every step is idempotent.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateHosts(hostNames); err != nil {
				return err
			}
			pick := !cmd.Flags().Changed("host") && stdinIsTerminal() && !render.JSONMode()
			return connect(g, hostNames, pick, noBrowser, local, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would change for each client, and change nothing.")
	cmd.Flags().StringSliceVar(&hostNames, "host", []string{"claude-code"}, "MCP host(s) to configure: "+hostChoices()+".")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the sign-in URL but don't open it.")
	cmd.Flags().BoolVar(&local, "local", false, "Use a machine-local API key from KEMORY_API_KEY instead of signing in (dev mode).")
	return cmd
}

func connect(g *globals, hostNames []string, pick, noBrowser, local, dryRun bool) error {
	if pick {
		g.screen = newScreen(g)
		g.screen.banner()
	}
	creds, err := auth.ValidCredentials(g.env, false)
	if err != nil {
		return err
	}
	if dryRun {
		switch {
		case creds == nil && g.screen != nil:
			g.screen.railOpen = true
			render.Echo(ui.Start(ui.Dim("Not signed in: a real run signs you in first")))
		case creds == nil:
			render.Muted(fmt.Sprintf("Dry run: would sign in to '%s' first.", g.env))
		default:
			g.screen.signedIn(g, creds)
		}
		if pick {
			if hostNames, err = pickHosts(g, creds); err != nil || len(hostNames) == 0 {
				return err
			}
		}
		return installHosts(g, creds, hostNames, "", "", true)
	}
	switch {
	case creds == nil:
		if g.screen == nil {
			render.Echo(fmt.Sprintf("No credentials for env '%s' — starting login…", g.env))
		}
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
	case g.screen == nil:
		render.Success(fmt.Sprintf("✓ Already logged in to '%s'", g.env))
	}
	g.screen.signedIn(g, creds)

	if pick {
		if hostNames, err = pickHosts(g, creds); err != nil || len(hostNames) == 0 {
			return err
		}
	}
	if err := installHosts(g, creds, hostNames, "", "", false); err != nil {
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
		who := orDefault(firstNonEmpty(m.Email, m.UserID), "your account")
		if g.screen != nil {
			g.screen.summary(who)
			return nil
		}
		render.Success("✓ Connected as " + who)
	}
	return nil
}
