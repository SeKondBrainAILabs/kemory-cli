// Package releases finds the newest published CLI release.
//
// Release binaries are published to the public Homebrew tap repository as
// `cli-vX.Y.Z` releases with `kemory-<os>-<arch>` assets, so the lookup needs
// no GitHub credentials.
package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// URL lists the tap's releases. Overridable in tests.
var URL = "https://api.github.com/repos/SeKondBrainAILabs/homebrew-s9n/releases?per_page=50"

// HTTPClient is used for the lookup. Overridable in tests.
var HTTPClient = &http.Client{Timeout: 5 * time.Second}

const tagPrefix = "cli-v"

// Latest returns the newest released CLI version, "" when none is published.
func Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	var list []struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", err
	}
	best := ""
	for _, r := range list {
		// The tap also carries other products' releases; only cli-v* tags with
		// kemory assets are this CLI's.
		if r.Draft || r.Prerelease || !strings.HasPrefix(r.TagName, tagPrefix) {
			continue
		}
		hasKemory := false
		for _, a := range r.Assets {
			if strings.HasPrefix(a.Name, "kemory-") {
				hasKemory = true
				break
			}
		}
		v := strings.TrimPrefix(r.TagName, tagPrefix)
		if !hasKemory || parse(v) == nil {
			continue
		}
		if best == "" || compare(parse(v), parse(best)) > 0 {
			best = v
		}
	}
	return best, nil
}

// parse returns the numeric parts of "X.Y.Z", or nil for anything else
// (a "dev" or snapshot build).
func parse(v string) []int {
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		out = append(out, n)
	}
	return out
}

func compare(a, b []int) int {
	for i := 0; i < max(len(a), len(b)); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

// IsNewer reports whether latest is newer than current, and false for ok when
// either is not a plain X.Y.Z version.
func IsNewer(latest, current string) (newer, ok bool) {
	l, c := parse(latest), parse(current)
	if l == nil || c == nil {
		return false, false
	}
	return compare(l, c) > 0, true
}
