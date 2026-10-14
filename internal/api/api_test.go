package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
)

// hostRouter answers every request in-process, keyed by host, so tests can
// use the real allowlisted cell hosts.
type hostRouter map[string]func(*http.Request) (int, string)

func (h hostRouter) RoundTrip(r *http.Request) (*http.Response, error) {
	handler, ok := h[r.URL.Host]
	if !ok {
		return nil, &netError{host: r.URL.Host}
	}
	status, body := handler(r)
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
}

type netError struct{ host string }

func (e *netError) Error() string { return "dial " + e.host + ": connection refused" }

func route(t *testing.T, h hostRouter) {
	t.Helper()
	old := HTTPClient
	HTTPClient = &http.Client{Transport: h}
	t.Cleanup(func() { HTTPClient = old })
}

func setHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func creds(url string) *config.Credentials {
	return &config.Credentials{
		AccessToken: "tok", RefreshToken: "rt", ExpiresAt: float64(time.Now().Add(time.Hour).Unix()),
		Issuer: "https://i", ClientID: "kemory-cli", KemoryURL: url, Env: "prod", ActiveOrgID: "org-7",
	}
}

const (
	zurich = "https://api.kemory.s9n.ai"
	mumbai = "https://api.kemory.in.sekondbrain.ai"
)

func refusal(target string) string {
	hosts := "null"
	if target != "" {
		hosts = `{"api": "` + target + `"}`
	}
	return `{"detail": {"code": "ORG_WRONG_RESIDENCY_CELL", "this_cell": "zurich", "target_hosts": ` + hosts + `}}`
}

func TestHeaders(t *testing.T) {
	Command = "whoami"
	defer func() { Command = "" }()
	h := Headers(creds(zurich))
	if h.Get("Authorization") != "Bearer tok" || h.Get("X-Organization-ID") != "org-7" || h.Get("X-Kemory-Command") != "whoami" {
		t.Fatalf("headers: %v", h)
	}
	if !strings.HasPrefix(h.Get("X-Kemory-Client"), "cli/") || !strings.HasPrefix(h.Get("User-Agent"), "kemory-cli/") {
		t.Fatalf("client headers: %v", h)
	}
}

func TestWrongCellRefusalIsRetriedOnceAgainstTheTargetAndSaved(t *testing.T) {
	setHome(t)
	var notice string
	Notice = func(s string) { notice = s }
	defer func() { Notice = func(string) {} }()
	var calls []string
	route(t, hostRouter{
		"api.kemory.s9n.ai": func(r *http.Request) (int, string) {
			calls = append(calls, "zurich "+r.Method)
			return 403, refusal(mumbai)
		},
		"api.kemory.in.sekondbrain.ai": func(r *http.Request) (int, string) {
			calls = append(calls, "mumbai "+r.Method)
			return 200, `{"ok": true}`
		},
	})
	c := creds(zurich)
	resp, err := Request(c, http.MethodPost, "/api/v1/memories", map[string]string{"content": "x"}, DefaultTimeout)
	if err != nil || resp.Status != 200 {
		t.Fatalf("got %v %v", resp, err)
	}
	if strings.Join(calls, ",") != "zurich POST,mumbai POST" {
		t.Fatalf("calls: %v", calls)
	}
	if c.KemoryURL != mumbai || !strings.Contains(notice, "mumbai cell") {
		t.Fatalf("not retargeted: %s %q", c.KemoryURL, notice)
	}
	if saved, _ := config.LoadCredentials("prod"); saved == nil || saved.KemoryURL != mumbai {
		t.Fatal("new cell not saved")
	}
}

func TestWithoutATargetTheOtherCellIsProbed(t *testing.T) {
	setHome(t)
	route(t, hostRouter{
		"api.kemory.s9n.ai":            func(*http.Request) (int, string) { return 403, refusal("") },
		"api.kemory.in.sekondbrain.ai": func(*http.Request) (int, string) { return 200, `{}` },
	})
	c := creds(zurich)
	if _, err := Get(c, "/api/v1/me"); err != nil || c.KemoryURL != mumbai {
		t.Fatalf("probe did not find the cell: %v %s", err, c.KemoryURL)
	}
}

func TestASecondRefusalStopsInsteadOfLooping(t *testing.T) {
	setHome(t)
	route(t, hostRouter{
		"api.kemory.s9n.ai":            func(*http.Request) (int, string) { return 403, refusal(mumbai) },
		"api.kemory.in.sekondbrain.ai": func(*http.Request) (int, string) { return 403, refusal(zurich) },
	})
	_, err := Get(creds(zurich), "/api/v1/me")
	if err == nil || !strings.Contains(err.Error(), "different residency region") {
		t.Fatalf("want an explanation, got %v", err)
	}
}

func TestASelfHostedURLIsNeverRewritten(t *testing.T) {
	setHome(t)
	route(t, hostRouter{"kemory.example.com": func(*http.Request) (int, string) { return 403, refusal(mumbai) }})
	c := creds("https://kemory.example.com")
	_, err := Get(c, "/api/v1/me")
	if err == nil || !strings.Contains(err.Error(), "left untouched") || c.KemoryURL != "https://kemory.example.com" {
		t.Fatalf("got %v, url %s", err, c.KemoryURL)
	}
}

func TestAnUnreachableHostIsANetworkError(t *testing.T) {
	route(t, hostRouter{})
	_, err := Get(creds(zurich), "/api/v1/me")
	if exits.Code(err) != exits.Network || !strings.Contains(err.Error(), "Cannot reach") {
		t.Fatalf("got %v", err)
	}
}

func TestFailMapsStatusToExitCode(t *testing.T) {
	err := Fail(&Response{Status: 404, Body: []byte("no such memory")}, "GET /x")
	if exits.Code(err) != exits.NotFound || err.Error() != "GET /x failed: 404 no such memory" {
		t.Fatalf("got %v (%d)", err, exits.Code(err))
	}
}
