package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/ui"
)

// pluginTools is the command each plugin host is installed through.
var pluginTools = map[string]string{"claude-code": "claude", "grok": "grok"}

// planned says what connecting host would do, without doing it. server is
// the name of the env's kemory entry.
func planned(env, host, server string, covered map[string]string) hostResult {
	if reason := covered[host]; reason != "" {
		if plan := planBesidePlugin(host, server); toolsOnlyPlugins[host] && plan != "" {
			return hostResult{host: host, detail: "already " + reason + "; would " + plan, ok: true}
		}
		return hostResult{host: host, detail: "already " + reason + " — nothing to do", ok: true}
	}
	note := ""
	if tool := pluginTools[host]; tool != "" && env == config.DefaultEnv {
		if _, err := lookPath(tool); err == nil {
			return hostResult{host: host, detail: "would install the Kemory plugin", ok: true}
		}
		note = errNoTool(tool).Error() + ", so no plugin"
	}
	detail := "would add the MCP entry to " + hosts.ResolveConfig(host)
	if h, ok := memoryHooks[host]; ok {
		detail += " and memory hooks to " + h.path()
	}
	if hosts.RunningCheck(host) {
		if hosts.InsideApp(host) {
			detail += ", from Terminal: this terminal runs inside the app, which has to quit"
		} else {
			detail += ", quitting and reopening the app"
		}
	}
	return hostResult{host: host, detail: joinNote(detail, note), ok: true}
}

// planBesidePlugin says what connecting a host covered by a tools-only plugin
// would still change, or "".
func planBesidePlugin(host, server string) string {
	path := hosts.ResolveConfig(host)
	dupes, needed := besidePlugin(host, path, server)
	var steps []string
	if len(dupes) > 0 {
		steps = append(steps, "remove the duplicate kemory entry from "+path)
	}
	if needed {
		steps = append(steps, "add memory hooks to "+memoryHooks[host].path())
	}
	return strings.Join(steps, " and ")
}

// pickRow is one detected host in the picker.
type pickRow struct {
	host, state, detail string
	// short is the status on the guided screen.
	short string
	// mark is how the guided screen shows the row, where that differs from
	// its state: a connected client connecting would improve, or one with a
	// problem.
	mark ui.ClientState
}

// commandMissing reports whether a server entry's command cannot be run from
// here: a program not on PATH, or a full path that no longer exists.
func commandMissing(command string) bool {
	if command == "" {
		return false
	}
	if filepath.IsAbs(command) {
		_, err := os.Stat(command)
		return err != nil
	}
	_, err := lookPath(command)
	return err != nil
}

// The two levels a client can have. automatic loads memory when a session
// starts and saves the session on its own; on demand leaves memory to the
// model, which uses Kemory's tools when it decides to.
const (
	levelFull  = "automatic"
	levelTools = "on demand"
)

// levelLegend says what the two levels mean, under the clients list.
const levelLegend = levelFull + ": loads and saves your memory on its own · " +
	levelTools + ": the AI uses memory when it decides"

// pluginLevel is what each host's Kemory plugin gives it. Grok Build has no
// session-start event, so its plugin cannot load memory on its own.
var pluginLevel = map[string]string{"claude-code": levelFull, "grok": levelTools}

// level is what a covered host gets from Kemory, from its coverage reason.
// A tools-only plugin is automatic once kemory's hooks are in.
func level(host, reason string) string {
	switch {
	case strings.Contains(reason, "connector"):
		return levelTools
	case toolsOnlyPlugins[host] && needsHooks(host):
		return levelTools
	}
	return orDefault(pluginLevel[host], levelFull)
}

// via is level with the way the host reaches Kemory, for doctor.
func via(host, reason string) string {
	if strings.Contains(reason, "connector") {
		return level(host, reason) + " · claude.ai connector"
	}
	return level(host, reason) + " · Kemory plugin"
}

// readyStatus says, briefly, what connecting a ready host would install.
func readyStatus(env, host string) string {
	status := levelTools
	switch {
	case pluginTools[host] != "" && env == config.DefaultEnv:
		if _, err := lookPath(pluginTools[host]); err == nil {
			status = level(host, "plugin")
		}
	case memoryHooks[host].install != nil:
		status = levelFull
	}
	if hosts.RunningCheck(host) {
		status += " · restarts the app"
	}
	return status
}

// pickHosts lists the hosts on this machine with their Kemory status and asks
// which to connect. Enter takes every host that is ready.
func pickHosts(g *globals, creds *config.Credentials) ([]string, error) {
	covered := coveredElsewhere(g.env, creds, time.Now())
	var rows []pickRow
	var missing []string
	for _, h := range hosts.Names {
		path := hosts.ResolveConfig(h)
		entries := named(hosts.KemoryEntries(h, path), g.profile.ServerName)
		reason := covered[h]
		var command string
		if len(entries) > 0 {
			command = hosts.EntryCommand(h, path, entries[0])
		}
		plan := ""
		if reason != "" && toolsOnlyPlugins[h] {
			plan = planBesidePlugin(h, g.profile.ServerName)
		}
		switch {
		case plan != "" && needsHooks(h):
			rows = append(rows, pickRow{
				host: h, state: "ready", mark: ui.Connected, detail: reason + "; " + plan,
				short: levelTools + " · connect to make it " + levelFull,
			})
		case plan != "":
			rows = append(rows, pickRow{
				host: h, state: "ready", mark: ui.Attention, detail: reason + "; " + plan,
				short: level(h, reason) + " · duplicate entry · connect to fix",
			})
		case reason != "" && len(entries) > 0:
			rows = append(rows, pickRow{
				host: h, state: "connected", mark: ui.Attention,
				detail: reason + "; " + path + " also has a duplicate entry, which `kemory doctor --fix` removes",
				short:  level(h, reason) + " · duplicate entry: run kemory doctor --fix",
			})
		case reason != "":
			rows = append(rows, pickRow{host: h, state: "connected", detail: reason, short: level(h, reason)})
		case !hosts.Installed(h):
			// A discontinued client is only listed where it is still installed.
			if !hosts.Discontinued(h) {
				missing = append(missing, h)
			}
		case len(entries) > 0 && commandMissing(command):
			// Connecting again writes the entry with a command that runs.
			rows = append(rows, pickRow{
				host: h, state: "ready", mark: ui.Attention,
				detail: path + " starts Kemory with " + command + ", which is not installed; connecting again fixes it",
				short:  "cannot start Kemory: " + filepath.Base(command) + " not found · connect to fix",
			})
		case len(entries) > 0 && needsHooks(h):
			rows = append(rows, pickRow{
				host: h, state: "ready", mark: ui.Connected,
				detail: "add automatic memory hooks (MCP entry already in " + path + ")",
				short:  levelTools + " · connect to make it " + levelFull,
			})
		case len(entries) > 0:
			short := levelTools
			if _, ok := memoryHooks[h]; ok {
				short = levelFull
			}
			rows = append(rows, pickRow{host: h, state: "connected", detail: "MCP entry in " + path, short: short})
		default:
			rows = append(rows, pickRow{
				host: h, state: "ready", mark: ui.Ready,
				detail: strings.TrimPrefix(planned(g.env, h, g.profile.ServerName, covered).detail, "would "),
				short:  readyStatus(g.env, h),
			})
		}
	}
	if g.screen != nil {
		return g.screen.pickFancy(rows, missing)
	}

	render.Heading("AI clients on this machine")
	var ready []int
	for i, r := range rows {
		render.Echo(fmt.Sprintf("  %2d. [%-9s] %-15s %s", i+1, r.state, r.host, r.detail))
		if r.state == "ready" {
			ready = append(ready, i+1)
		}
	}
	if len(missing) > 0 {
		render.Muted("      not found: " + strings.Join(missing, ", "))
	}
	render.Echo("")
	if len(ready) == 0 {
		render.Success("Every client found here is already connected.")
		return nil, nil
	}

	fmt.Fprintf(render.Err, "Connect which? Enter for all ready (%s), numbers like %q, or \"none\": ",
		joinInts(ready), joinInts(ready[:1]))
	line, _ := bufio.NewReader(Stdin).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	var picked []string
	switch answer {
	case "":
		for _, n := range ready {
			picked = append(picked, rows[n-1].host)
		}
	case "none", "n", "no":
		render.Muted("Nothing connected.")
		return nil, nil
	default:
		for _, field := range strings.FieldsFunc(answer, func(r rune) bool { return r == ' ' || r == ',' }) {
			n, err := strconv.Atoi(field)
			if err != nil || n < 1 || n > len(rows) {
				return nil, exits.New(fmt.Sprintf("%q is not one of the numbers listed (1-%d)", field, len(rows)), exits.Usage)
			}
			if !contains(picked, rows[n-1].host) {
				picked = append(picked, rows[n-1].host)
			}
		}
	}
	render.Echo("")
	return picked, nil
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, " ")
}
