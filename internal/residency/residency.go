// Package residency handles Kemory's residency cells.
//
// Kemory prod runs as residency cells, and an org's data region is fixed
// server-side. A request for an org in another cell is refused with
//
//	403 {"detail": {"code": "ORG_WRONG_RESIDENCY_CELL", "this_cell": "zurich",
//	                "target_hosts": {"api": ..., "dashboard": ...} | null}}
//
// The server refuses before any route body runs, so retrying once against the
// org's cell is safe, POSTs included. target_hosts arrives in a response body
// and is untrusted: a bearer token is only ever sent to a host on the exact
// allowlist below.
package residency

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// RefusalCode is the detail.code of a wrong-cell refusal.
const RefusalCode = "ORG_WRONG_RESIDENCY_CELL"

// KnownCellAPIHosts maps the server's data_region value (the refusal's
// this_cell) to that cell's direct API host. Never list browser dashboard
// hosts here: they redirect every API request to a login page.
var KnownCellAPIHosts = map[string]string{
	"zurich": "https://api.kemory.s9n.ai",
	"mumbai": "https://api.kemory.in.sekondbrain.ai",
}

// knownCellDashboardHosts are used in user-facing messages only, never for
// API traffic.
var knownCellDashboardHosts = map[string]bool{
	"https://kemory.sekondbrain.ai":    true,
	"https://kemory.in.sekondbrain.ai": true,
}

// IsAllowedAPIURL reports whether u is exactly an allowlisted cell API host.
func IsAllowedAPIURL(u string) bool {
	u = strings.TrimRight(u, "/")
	for _, host := range KnownCellAPIHosts {
		if host == u {
			return true
		}
	}
	return false
}

// Refusal is a parsed, validated wrong-cell refusal. TargetAPI and
// TargetDashboard are empty unless the server's value passed validation.
type Refusal struct {
	ThisCell        string
	TargetAPI       string
	TargetDashboard string
}

// normalize returns "https://<host>" or "" when the URL has any other shape:
// another scheme, userinfo, a port, a path, a query or a fragment.
func normalize(candidate string) string {
	u, err := url.Parse(candidate)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return ""
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" {
		return ""
	}
	return "https://" + strings.ToLower(u.Hostname())
}

// IsSafeCellTarget reports whether candidate normalizes to an allowlisted API
// host.
func IsSafeCellTarget(candidate string) bool {
	n := normalize(candidate)
	return n != "" && IsAllowedAPIURL(n)
}

// ParseRefusal returns the refusal in a response, or nil for anything else.
// Only a 403 whose JSON body carries the exact refusal code counts; every
// other 403 keeps its normal handling.
func ParseRefusal(status int, body []byte) *Refusal {
	if status != http.StatusForbidden {
		return nil
	}
	var payload struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	var detail struct {
		Code        string          `json:"code"`
		ThisCell    any             `json:"this_cell"`
		TargetHosts json.RawMessage `json:"target_hosts"`
	}
	if json.Unmarshal(payload.Detail, &detail) != nil || detail.Code != RefusalCode {
		return nil
	}
	r := &Refusal{}
	if detail.ThisCell != nil {
		r.ThisCell = fmt.Sprint(detail.ThisCell)
	}
	var hosts map[string]any
	if json.Unmarshal(detail.TargetHosts, &hosts) == nil {
		if api, ok := hosts["api"].(string); ok && IsSafeCellTarget(api) {
			r.TargetAPI = normalize(api)
		}
		if dash, ok := hosts["dashboard"].(string); ok {
			if n := normalize(dash); n != "" && knownCellDashboardHosts[n] {
				r.TargetDashboard = n
			}
		}
	}
	return r
}

// CandidateHosts lists the hosts worth trying after a refusal: the validated
// server target, or else every allowlisted host except the one that refused
// (by URL and by region name).
func CandidateHosts(r *Refusal, currentURL string) []string {
	current := strings.TrimRight(currentURL, "/")
	if r.TargetAPI != "" && r.TargetAPI != current {
		return []string{r.TargetAPI}
	}
	refusing := KnownCellAPIHosts[r.ThisCell]
	var out []string
	for _, region := range sortedRegions() {
		u := KnownCellAPIHosts[region]
		if u != current && u != refusing {
			out = append(out, u)
		}
	}
	return out
}

func sortedRegions() []string {
	regions := make([]string, 0, len(KnownCellAPIHosts))
	for r := range KnownCellAPIHosts {
		regions = append(regions, r)
	}
	sort.Strings(regions)
	return regions
}

// RegionFor returns the region name of an allowlisted API host, or "another".
func RegionFor(apiURL string) string {
	for region, u := range KnownCellAPIHosts {
		if u == apiURL {
			return region
		}
	}
	return "another"
}

// WrongRegionMessage explains a refusal the CLI could not recover from.
func WrongRegionMessage(r *Refusal, currentURL string) string {
	lines := []string{fmt.Sprintf(
		"This organisation's data is stored in a different residency region, and %s (the '%s' cell) cannot serve it.",
		currentURL, r.ThisCell)}
	if r.TargetAPI != "" {
		lines = append(lines, "Its API endpoint is "+r.TargetAPI+".")
	}
	if r.TargetDashboard != "" {
		lines = append(lines, "Its dashboard is "+r.TargetDashboard+".")
	}
	if r.TargetAPI == "" {
		known := make([]string, 0, len(KnownCellAPIHosts))
		for _, region := range sortedRegions() {
			known = append(known, region+": "+KnownCellAPIHosts[region])
		}
		lines = append(lines,
			"Cells known to this CLI — "+strings.Join(known, ", ")+".",
			"If your region is missing here, upgrade the CLI (`kemory upgrade`) or point KEMORY_URL at your region's API host.")
	}
	return strings.Join(lines, "\n")
}

// HTTPClient is the client discovery uses. Overridable in tests.
var HTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	// Never follow a redirect with credentials attached.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// fetchCellDirectory returns region -> validated API host from
// GET {base}/.well-known/kemory-cells, or nil on any failure. The directory
// extends what this build knows; it never gates it.
func fetchCellDirectory(baseURL string) map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), HTTPClient.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/.well-known/kemory-cells", nil)
	if err != nil {
		return nil
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body struct {
		Cells map[string]json.RawMessage `json:"cells"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return nil
	}
	out := map[string]string{}
	for region, raw := range body.Cells {
		var hosts map[string]any
		if json.Unmarshal(raw, &hosts) != nil {
			continue
		}
		if api, ok := hosts["api"].(string); ok {
			if n := normalize(api); n != "" {
				out[region] = n
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SelectionOverridesDiscovery reports whether DiscoverHomeCell's answer is
// about the wrong org. Discovery answers for the token's org, so when the user
// has selected a different org, acting on it would route to the wrong cell.
func SelectionOverridesDiscovery(activeOrgID, tokenOrgID string) bool {
	return activeOrgID != "" && activeOrgID != tokenOrgID
}

// DiscoverHomeCell asks GET {base}/api/v1/residency/cell where the token's org
// lives. It returns a validated API host only when the server says this is not
// the org's home cell, and "" for "stay put" (home cell, endpoint missing,
// any failure). The host must be allowlisted or appear in the cell directory
// served by the same base URL.
func DiscoverHomeCell(baseURL string, headers http.Header) string {
	ctx, cancel := context.WithTimeout(context.Background(), HTTPClient.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/v1/residency/cell", nil)
	if err != nil {
		return ""
	}
	req.Header = headers.Clone()
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var body struct {
		IsHomeCell *bool          `json:"is_home_cell"`
		CellHosts  map[string]any `json:"cell_hosts"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil || body.IsHomeCell == nil || *body.IsHomeCell {
		return ""
	}
	api, ok := body.CellHosts["api"].(string)
	if !ok {
		return ""
	}
	n := normalize(api)
	if n == "" || n == strings.TrimRight(baseURL, "/") {
		return ""
	}
	if IsAllowedAPIURL(n) {
		return n
	}
	for _, host := range fetchCellDirectory(baseURL) {
		if host == n {
			return n
		}
	}
	return ""
}
