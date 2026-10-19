package releases

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(t *testing.T, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	old, oldNPM := URL, NPMURL
	URL, NPMURL = srv.URL, srv.URL+"/npm"
	t.Cleanup(func() { URL, NPMURL = old, oldNPM })
}

func TestLatestPicksTheNewestCLIRelease(t *testing.T) {
	serve(t, http.StatusOK, `[
		{"tag_name": "cli-v0.6.9", "assets": [{"name": "kemory-linux-x64.tar.gz"}]},
		{"tag_name": "cli-v0.6.14", "assets": [{"name": "kemory-macos-arm64.tar.gz"}]},
		{"tag_name": "cli-v0.7.0", "prerelease": true, "assets": [{"name": "kemory-linux-x64.tar.gz"}]},
		{"tag_name": "cli-v9.0.0", "assets": [{"name": "s9n-linux-x64.tar.gz"}]},
		{"tag_name": "v3.0.0", "assets": [{"name": "kemory-linux-x64.tar.gz"}]}
	]`)
	got, err := Latest(context.Background())
	if err != nil || got != "0.6.14" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestLatestReportsHTTPErrors(t *testing.T) {
	serve(t, http.StatusForbidden, `{"message": "rate limited"}`)
	if _, err := Latest(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

func TestLatestFallsBackToNPMWhenGitHubRefuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/npm" {
			_, _ = w.Write([]byte(`{"name": "kemory", "version": "0.18.7"}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	old, oldNPM := URL, NPMURL
	URL, NPMURL = srv.URL, srv.URL+"/npm"
	t.Cleanup(func() { URL, NPMURL = old, oldNPM })
	got, err := Latest(context.Background())
	if err != nil || got != "0.18.7" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		newer, ok       bool
	}{
		{"0.6.14", "0.6.9", true, true},
		{"0.6.9", "0.6.14", false, true},
		{"1.0", "1.0.0", false, true},
		{"0.7.0", "dev", false, false},
		{"0.6.14", "0.7.0-dev", false, true},
		{"0.7.0", "0.7.0+abc123", false, true},
		{"0.7.0", "0.0.0-SNAPSHOT-abc", true, true},
	}
	for _, c := range cases {
		if newer, ok := IsNewer(c.latest, c.current); newer != c.newer || ok != c.ok {
			t.Fatalf("IsNewer(%s, %s) = %v, %v", c.latest, c.current, newer, ok)
		}
	}
}
