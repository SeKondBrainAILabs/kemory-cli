package cli

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

// pluginTools is the command each plugin host is installed through.
var pluginTools = map[string]string{"claude-code": "claude", "grok": "grok"}

// planned says what connecting host would do, without doing it.
func planned(env, host string, covered map[string]string) hostResult {
	if reason := covered[host]; reason != "" {
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

// pickRow is one detected host in the picker.
type pickRow struct {
	host, state, detail string
	// short is the status on the guided screen.
	short string
}

// via names the way a covered host reaches Kemory, from its coverage reason.
func via(reason string) string {
	if strings.Contains(reason, "connector") {
		return "claude.ai connector"
	}
	return "Kemory plugin"
}

// readyStatus says, briefly, what connecting a ready host would install.
func readyStatus(env, host string) string {
	status := "adds memory tools"
	switch {
	case pluginTools[host] != "" && env == config.DefaultEnv:
		if _, err := lookPath(pluginTools[host]); err == nil {
			status = "adds the Kemory plugin: tools + automatic memory"
		}
	case memoryHooks[host].install != nil:
		status = "adds memory tools + automatic memory"
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
		switch {
		case reason != "" && len(entries) > 0:
			rows = append(rows, pickRow{
				h, "connected", reason + "; " + path +
					" also has a duplicate entry, which `kemory doctor --fix` removes",
				via(reason) + " · duplicate entry: run kemory doctor --fix",
			})
		case reason != "":
			rows = append(rows, pickRow{h, "connected", reason, via(reason)})
		case !hosts.Installed(h):
			// A discontinued client is only listed where it is still installed.
			if !hosts.Discontinued(h) {
				missing = append(missing, h)
			}
		case len(entries) > 0 && needsHooks(h):
			rows = append(rows, pickRow{
				h, "ready", "add automatic memory hooks (MCP entry already in " + path + ")",
				"adds automatic memory",
			})
		case len(entries) > 0:
			short := "memory tools"
			if _, ok := memoryHooks[h]; ok {
				short = "memory tools + automatic memory"
			}
			rows = append(rows, pickRow{h, "connected", "MCP entry in " + path, short})
		default:
			rows = append(rows, pickRow{
				h, "ready", strings.TrimPrefix(planned(g.env, h, covered).detail, "would "),
				readyStatus(g.env, h),
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
