package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/releases"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/residency"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/version"
)

// HealthClient probes /health/live. Overridable in tests.
var HealthClient = &http.Client{
	Timeout:       5 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

type checkResult struct {
	Check  string  `json:"check"`
	Status string  `json:"status"`
	Detail *string `json:"detail"`
}

type doctor struct {
	g      *globals
	fix    bool
	yes    bool
	checks []checkResult
}

func (d *doctor) emit(label string, status render.Status, detail string) {
	var dp *string
	if detail != "" {
		dp = &detail
	}
	d.checks = append(d.checks, checkResult{Check: label, Status: string(status), Detail: dp})
	render.Check(label, status, detail)
}

func newDoctorCmd(g *globals) *cobra.Command {
	var fix, yes bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the setup end to end: sign-in, network, API and MCP hosts.",
		Long: "Run this first when something feels wrong. Each line ends in PASS, FAIL or\n" +
			"SKIP with a hint on what to do next. No personal data is printed.\n\n" +
			"doctor exits 0 even when a check fails: it ran fine, the setup did not.\n" +
			"With --output json, \"ok\" says whether every check passed.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return (&doctor{g: g, fix: fix, yes: yes}).run()
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "Wire every installed MCP host that lacks kemory, and remove entries for hosts that are not installed or that duplicate another connection.")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "With --fix, quit and reopen Claude Desktop without asking when its config must change.")
	return cmd
}

func (d *doctor) run() error {
	creds, err := config.LoadCredentials(d.g.env)
	if err != nil {
		return err
	}
	kemoryURL := d.g.profile.KemoryURL
	if creds != nil && creds.KemoryURL != "" {
		kemoryURL = creds.KemoryURL
	}

	render.Heading("kemory doctor")
	render.KVs([]render.KV{
		{Key: "  os", Value: runtime.GOOS + "/" + runtime.GOARCH},
		{Key: "  cli", Value: version.Version},
		{Key: "  kemory", Value: kemoryURL},
	})
	render.Echo("")

	d.checkCredentials(creds)
	d.checkReachable(kemoryURL)
	fresh := d.checkRefresh(creds)
	d.checkRoundTrip(fresh, kemoryURL)
	fixHint := d.checkHosts(fresh)
	d.checkFreshness()

	if render.JSONMode() {
		ok := true
		for _, c := range d.checks {
			if c.Status == string(render.Fail) {
				ok = false
			}
		}
		out, err := json.MarshalIndent(map[string]any{
			"cli": version.Version, "kemory_url": kemoryURL, "ok": ok, "checks": d.checks,
		}, "", "  ")
		if err != nil {
			return err
		}
		render.JSON(out)
		return nil
	}
	render.Echo("")
	if fixHint {
		render.Muted("To connect every installed MCP host and clean up the rest, run:")
		render.Echo("  kemory doctor --fix")
		render.Echo("")
	}
	render.Muted("Done. If a check failed, the hint after FAIL tells you what to do.")
	return nil
}

func (d *doctor) checkCredentials(creds *config.Credentials) {
	if creds == nil {
		d.emit("credentials cached", render.Fail, "run `kemory login` (or `kemory login --local`)")
		return
	}
	path, _ := config.CredentialsPath(d.g.env)
	status := render.Pass
	if creds.AccessToken == "" {
		status = render.Fail
	}
	d.emit("credentials cached", status, fmt.Sprintf("%s, access token %s", path, expiry(creds.ExpiresAt, time.Now())))
}

func (d *doctor) checkReachable(kemoryURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), HealthClient.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(kemoryURL, "/")+"/health/live", nil)
	if err != nil {
		d.emit("kemory reachable", render.Fail, err.Error())
		return
	}
	resp, err := HealthClient.Do(req)
	if err != nil {
		d.emit("kemory reachable", render.Fail, err.Error())
		return
	}
	_ = resp.Body.Close()
	status := render.Fail
	if resp.StatusCode == http.StatusOK {
		status = render.Pass
	}
	d.emit("kemory reachable", status, fmt.Sprintf("GET /health/live → %d", resp.StatusCode))
}

// checkRefresh exercises the refresh path every command uses, so an access
// token that merely expired (they live minutes) does not read as broken.
func (d *doctor) checkRefresh(creds *config.Credentials) *config.Credentials {
	switch {
	case creds == nil:
		d.emit("token refresh", render.Skip, "no credentials")
		return nil
	case creds.IsLocal():
		d.emit("token refresh", render.Skip, "local-mode credential — refresh not applicable")
		return creds
	}
	fresh, err := auth.ValidCredentials(d.g.env, true)
	if err != nil {
		d.emit("token refresh", render.Fail, err.Error())
		return nil
	}
	if fresh == nil {
		d.emit("token refresh", render.Fail, "refresh failed — the session expired or was revoked. Run `kemory login`.")
		return nil
	}
	d.emit("token refresh", render.Pass, "new access token "+expiry(fresh.ExpiresAt, time.Now()))
	return fresh
}

// expiry says when an epoch-seconds expiry falls, in local time and relative
// to now: "expires 19:24 (in 59m)", with the date when it is not today.
func expiry(epoch float64, now time.Time) string {
	at := time.Unix(int64(epoch), 0).In(now.Location())
	when := at.Format("15:04")
	if y, m, d := at.Date(); y != now.Year() || m != now.Month() || d != now.Day() {
		when = at.Format("2006-01-02 15:04")
	}
	left := at.Sub(now)
	if left < 0 {
		return fmt.Sprintf("expired %s (%s ago)", when, roughDuration(-left))
	}
	return fmt.Sprintf("expires %s (in %s)", when, roughDuration(left))
}

// roughDuration renders d to the minute: "45s" below a minute, then "59m",
// "1h", "1h5m" or "3d2h".
func roughDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	d = d.Round(time.Minute)
	days, hours, mins := int(d/(24*time.Hour)), int(d/time.Hour)%24, int(d/time.Minute)%60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%dh%dm", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dm", mins)
}

// checkRoundTrip asks /api/v1/me the same question every command asks,
// including the selected org. A wrong-cell refusal is followed the way a
// command would follow it; doctor reports where the identity resolves but
// does not rewrite the credentials file.
func (d *doctor) checkRoundTrip(fresh *config.Credentials, kemoryURL string) {
	if fresh == nil || fresh.AccessToken == "" {
		d.emit("auth round-trip", render.Skip, "no usable credentials")
		return
	}
	probe := *fresh
	probe.KemoryURL = kemoryURL
	status, body, err := getMe(&probe)
	if err != nil {
		d.emit("auth round-trip", render.Fail, err.Error())
		return
	}
	switch status {
	case http.StatusOK:
		d.emit("auth round-trip", render.Pass, meSummary(body))
	case http.StatusUnauthorized:
		d.emit("auth round-trip", render.Fail, "401 — the server rejected a fresh token. Check that the Kemory server "+
			"trusts this identity provider and that the token's audience includes the Kemory API.")
	default:
		refusal := residency.ParseRefusal(status, body)
		if refusal == nil {
			d.emit("auth round-trip", render.Fail, fmt.Sprintf("GET /api/v1/me → %d", status))
			return
		}
		for _, candidate := range residency.CandidateHosts(refusal, kemoryURL) {
			probe.KemoryURL = candidate
			if s, b, err := getMe(&probe); err == nil && s == http.StatusOK {
				d.emit("auth round-trip", render.Pass, fmt.Sprintf("%s — served by %s. This CLI's saved URL still points at "+
					"%s (the '%s' cell); the next command switches and saves it.", meSummary(b), candidate, kemoryURL, refusal.ThisCell))
				return
			}
		}
		detail := fmt.Sprintf("403 — this org's data lives in a different residency cell (this CLI points at %s, the '%s' cell", kemoryURL, refusal.ThisCell)
		if refusal.TargetAPI != "" {
			detail += "; the org is served by " + refusal.TargetAPI
		}
		detail += "). No allowlisted cell could confirm it."
		d.emit("auth round-trip", render.Fail, detail)
	}
}

func getMe(creds *config.Credentials) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(creds.KemoryURL, "/")+"/api/v1/me", nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header = api.Headers(creds)
	resp, err := api.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, err
}

func meSummary(body []byte) string {
	var m struct {
		OrgID string            `json:"org_id"`
		Teams []json.RawMessage `json:"teams"`
	}
	_ = json.Unmarshal(body, &m)
	return fmt.Sprintf("org=%s, teams=%d", orDefault(m.OrgID, "?"), len(m.Teams))
}

// checkHosts reports each MCP host and returns whether `doctor --fix` would
// change something. An entry only counts where the host app is installed.
func (d *doctor) checkHosts(creds *config.Credentials) bool {
	wired, fixable := 0, false
	launch := hosts.LaunchFor(d.g.env)
	covered := coveredElsewhere(d.g.env, creds, time.Now())
	for _, host := range hosts.Names {
		installed := hosts.Installed(host)
		path := hosts.ResolveConfig(host)
		entries := hosts.KemoryEntries(host, path)
		label := "mcp host: " + host
		reason := covered[host]
		dupes := named(entries, d.g.profile.ServerName)
		switch {
		case reason != "" && len(dupes) > 0:
			wired++
			if d.removeDuplicate(label, host, path, reason, dupes) {
				fixable = true
			}
		case reason != "" && installed:
			wired++
			d.emit(label, render.Pass, reason)
		case len(entries) > 0 && installed && needsHooks(host):
			wired++
			if !d.fix {
				fixable = true
				d.emit(label, render.Skip, path+" — tools only, no automatic memory. `kemory doctor --fix` adds the hooks")
				continue
			}
			hooks, err := addHooks(host, launch)
			if err != nil {
				d.emit(label, render.Fail, "memory hooks not added: "+err.Error())
				continue
			}
			d.emit(label, render.Pass, joinNote(path, hooks))
		case len(entries) > 0 && installed:
			wired++
			detail := path
			if _, ok := memoryHooks[host]; ok {
				detail += " — automatic memory hooks installed"
			}
			d.emit(label, render.Pass, detail)
		case len(entries) > 0:
			if !d.fix {
				fixable = true
				d.emit(label, render.Skip, fmt.Sprintf("%s is not installed but %s has a kemory entry", host, path))
				continue
			}
			if err := hosts.RemoveEntries(host, path, entries); err != nil {
				d.emit(label, render.Fail, err.Error())
				continue
			}
			d.emit(label, render.Pass, "removed kemory entry — "+host+" is not installed")
		case installed:
			if !d.fix {
				fixable = true
				d.emit(label, render.Skip, "installed, not connected")
				continue
			}
			plugin, pluginErr := tryPlugin(d.g.env, host)
			if plugin {
				wired++
				d.emit(label, render.Pass, pluginInstalled(host))
				continue
			}
			if pluginErr != nil {
				d.emit(label+" plugin", render.Skip, pluginFallback(pluginErr))
			}
			done, note, err := withAppClosed(host, d.yes, resumeArgs(d.g.env, "doctor", "--fix"), func() error {
				return hosts.WriteEntry(host, path, d.g.profile.ServerName, launch)
			})
			switch {
			case err != nil:
				d.emit(label, render.Fail, err.Error())
			case !done:
				d.emit(label, render.Skip, "installed, not connected — "+note)
			default:
				wired++
				detail := joinNote(fmt.Sprintf("wrote kemory entry into %s", path), orDefault(note, "restart "+host))
				if hooks, err := addHooks(host, launch); err != nil {
					detail = joinNote(detail, "memory hooks not added: "+err.Error())
				} else {
					detail = joinNote(detail, hooks)
				}
				d.emit(label, render.Pass, detail)
			}
		}
	}
	if wired == 0 {
		d.emit("mcp host config", render.Fail, "no installed MCP host has a kemory entry")
	}
	return !d.fix && (wired == 0 || fixable)
}

// removeDuplicate handles a host that reaches Kemory another way and also has
// a kemory entry, which gives it every tool twice. It reports whether
// `doctor --fix` would remove the entry.
func (d *doctor) removeDuplicate(label, host, path, reason string, dupes []string) bool {
	dup := fmt.Sprintf("duplicate: already %s, and %s also has a kemory entry", reason, path)
	if !d.fix {
		d.emit(label, render.Skip, dup+". `kemory doctor --fix` removes it.")
		return true
	}
	done, note, err := withAppClosed(host, d.yes, resumeArgs(d.g.env, "doctor", "--fix"), func() error {
		return hosts.RemoveEntries(host, path, dupes)
	})
	switch {
	case err != nil:
		d.emit(label, render.Fail, err.Error())
	case !done:
		d.emit(label, render.Skip, dup+". "+note)
	default:
		d.emit(label, render.Pass, joinNote(fmt.Sprintf("removed the duplicate entry from %s; %s", path, reason), orDefault(note, "restart "+host)))
	}
	return false
}

// checkFreshness compares this build with the newest release. A failed lookup
// is a SKIP: doctor must not need GitHub to be up.
func (d *doctor) checkFreshness() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	latest, err := releases.Latest(ctx)
	switch {
	case err != nil:
		var netErr interface{ Timeout() bool }
		if errors.As(err, &netErr) {
			d.emit("cli up-to-date", render.Skip, "could not reach github.com in time")
			return
		}
		d.emit("cli up-to-date", render.Skip, "could not check: "+err.Error())
	case latest == "":
		d.emit("cli up-to-date", render.Skip, "no release published yet")
	default:
		newer, ok := releases.IsNewer(latest, version.Version)
		switch {
		case !ok:
			d.emit("cli up-to-date", render.Skip, fmt.Sprintf("could not compare %q with %q", version.Version, latest))
		case newer:
			d.emit("cli up-to-date", render.Fail, fmt.Sprintf("version %s is available — run `kemory upgrade`", latest))
		default:
			d.emit("cli up-to-date", render.Pass, "latest is "+latest)
		}
	}
}
