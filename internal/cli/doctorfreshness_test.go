package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/releases"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/version"
)

func TestDoctorFreshnessCheck(t *testing.T) {
	const newer = `[{"tag_name": "cli-v9.0.0", "assets": [{"name": "kemory-linux-x64.tar.gz"}]}]`
	cases := []struct {
		name   string
		handle http.HandlerFunc
		status string
		detail string
	}{
		{"newer release", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(newer)) },
			"fail", "version 9.0.0 is available — run `kemory upgrade`"},
		{"same release", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Replace(newer, "9.0.0", "1.2.3", 1)))
		}, "pass", "latest is 1.2.3"},
		{"server error", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
			"skip", "could not check: GitHub returned HTTP 500"},
		{"slow reply", func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() },
			"skip", "could not reach github.com in time"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handle)
			t.Cleanup(srv.Close)

			oldV, oldURL, oldNPM, oldClient := version.Version, releases.URL, releases.NPMURL, releases.HTTPClient
			t.Cleanup(func() {
				version.Version, releases.URL, releases.NPMURL, releases.HTTPClient = oldV, oldURL, oldNPM, oldClient
			})
			version.Version = "1.2.3"
			releases.URL, releases.NPMURL = srv.URL, srv.URL+"/npm"
			releases.HTTPClient = &http.Client{Timeout: 100 * time.Millisecond}

			d := &doctor{}
			d.checkFreshness()
			if len(d.checks) != 1 {
				t.Fatalf("checks = %+v", d.checks)
			}
			got := d.checks[0]
			if got.Check != "cli up-to-date" || got.Status != tc.status || got.Detail == nil || *got.Detail != tc.detail {
				detail := "<nil>"
				if got.Detail != nil {
					detail = *got.Detail
				}
				t.Fatalf("got %s %s %q, want %s %q", got.Check, got.Status, detail, tc.status, tc.detail)
			}
		})
	}
}
