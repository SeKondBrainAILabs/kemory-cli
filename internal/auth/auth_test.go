package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
)

func setHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

func noSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var slept []time.Duration
	old := Sleep
	Sleep = func(d time.Duration) { slept = append(slept, d) }
	t.Cleanup(func() { Sleep = old })
	return &slept
}

// fakeIssuer answers the device endpoint, then the token endpoint with the
// given sequence of (status, body) replies.
type fakeIssuer struct {
	mu       sync.Mutex
	replies  [][2]string
	forms    []map[string]string
	redirect bool
}

func (f *fakeIssuer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	form := map[string]string{}
	for k := range r.PostForm {
		form[k] = r.PostForm.Get(k)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forms = append(f.forms, form)
	if f.redirect {
		w.Header().Set("Location", "https://new-issuer.example.com")
		w.WriteHeader(http.StatusFound)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/auth/device") {
		w.Write([]byte(`{"device_code": "dc", "user_code": "ABCD-EFGH", "verification_uri": "https://idp/device", "expires_in": 600, "interval": 1}`))
		return
	}
	reply := f.replies[0]
	f.replies = f.replies[1:]
	code := 200
	if reply[0] != "200" {
		code = 400
	}
	w.WriteHeader(code)
	w.Write([]byte(reply[1]))
}

func TestPKCEChallengeIsS256OfTheVerifier(t *testing.T) {
	verifier, challenge, err := PKCEPair()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(verifier))
	if challenge != base64.RawURLEncoding.EncodeToString(sum[:]) || len(verifier) < 43 {
		t.Fatal("challenge is not S256(verifier)")
	}
}

func TestLoginPollsThroughPendingAndSlowDownThenSaves(t *testing.T) {
	setHome(t)
	slept := noSleep(t)
	issuer := &fakeIssuer{replies: [][2]string{
		{"400", `{"error": "authorization_pending"}`},
		{"400", `{"error": "slow_down"}`},
		{"200", `{"access_token": "at", "refresh_token": "rt", "expires_in": 300}`},
	}}
	srv := httptest.NewServer(issuer)
	defer srv.Close()
	opened := ""
	OpenBrowser = func(u string) error { opened = u; return nil }
	t.Setenv("KEMORY_HEADLESS", "")
	t.Setenv("DISPLAY", ":0")

	creds, err := Login(srv.URL, "kemory-cli", "https://api.kemory.s9n.ai", "prod", true, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != "at" || creds.RefreshToken != "rt" || creds.KemoryURL != "https://api.kemory.s9n.ai" {
		t.Fatalf("creds: %+v", creds)
	}
	if opened != "https://idp/device" {
		t.Fatalf("browser opened %q", opened)
	}
	if len(*slept) != 2 || (*slept)[0] != time.Second || (*slept)[1] != 6*time.Second {
		t.Fatalf("slow_down must add 5s to the interval: %v", *slept)
	}
	device, poll := issuer.forms[0], issuer.forms[1]
	if device["code_challenge_method"] != "S256" || device["code_challenge"] == "" || device["scope"] != DefaultScope {
		t.Fatalf("device request: %v", device)
	}
	if poll["code_verifier"] == "" || poll["grant_type"] != "urn:ietf:params:oauth:grant-type:device_code" {
		t.Fatalf("token poll: %v", poll)
	}
	saved, _ := config.LoadCredentials("prod")
	if saved == nil || saved.AccessToken != "at" {
		t.Fatal("credentials not saved")
	}
}

func TestDeniedLoginIsAFlowError(t *testing.T) {
	setHome(t)
	noSleep(t)
	srv := httptest.NewServer(&fakeIssuer{replies: [][2]string{{"400", `{"error": "access_denied", "error_description": "no"}`}}})
	defer srv.Close()
	_, err := Login(srv.URL, "kemory-cli", "https://k", "prod", false, io.Discard)
	var flow *FlowError
	if !errors.As(err, &flow) || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("want access_denied FlowError, got %v", err)
	}
}

func TestIssuerRedirectIsNotReportedAsAnExpiredSession(t *testing.T) {
	setHome(t)
	srv := httptest.NewServer(&fakeIssuer{redirect: true})
	defer srv.Close()
	_, err := RequestDeviceCode(srv.URL, "kemory-cli", "")
	var redirect *RedirectError
	if !errors.As(err, &redirect) || redirect.Location != "https://new-issuer.example.com" || !strings.Contains(err.Error(), "NOT an expired session") {
		t.Fatalf("want RedirectError naming the target, got %v", err)
	}
}

func saveCreds(t *testing.T, issuer string, expiresIn time.Duration) *config.Credentials {
	t.Helper()
	c := &config.Credentials{
		AccessToken: "old", RefreshToken: "rt-old", ExpiresAt: float64(time.Now().Add(expiresIn).Unix()),
		Issuer: issuer, ClientID: "kemory-cli", KemoryURL: "https://k", Env: "prod",
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestValidCredentials(t *testing.T) {
	t.Run("no file", func(t *testing.T) {
		setHome(t)
		if c, err := ValidCredentials("prod", false); c != nil || err != nil {
			t.Fatalf("got %v %v", c, err)
		}
	})
	t.Run("fresh token is not refreshed", func(t *testing.T) {
		setHome(t)
		saveCreds(t, "https://unused.invalid", time.Hour)
		if c, err := ValidCredentials("prod", false); err != nil || c.AccessToken != "old" {
			t.Fatalf("got %v %v", c, err)
		}
	})
	t.Run("expiring token is refreshed and saved", func(t *testing.T) {
		setHome(t)
		srv := httptest.NewServer(&fakeIssuer{replies: [][2]string{{"200", `{"access_token": "new", "expires_in": 300}`}}})
		defer srv.Close()
		saveCreds(t, srv.URL, 10*time.Second)
		c, err := ValidCredentials("prod", false)
		if err != nil || c.AccessToken != "new" || c.RefreshToken != "rt-old" {
			t.Fatalf("got %+v %v", c, err)
		}
		if saved, _ := config.LoadCredentials("prod"); saved.AccessToken != "new" {
			t.Fatal("refreshed token not saved")
		}
	})
	t.Run("rejected refresh means log in again", func(t *testing.T) {
		setHome(t)
		srv := httptest.NewServer(&fakeIssuer{replies: [][2]string{{"400", `{"error": "invalid_grant"}`}}})
		defer srv.Close()
		saveCreds(t, srv.URL, 0)
		if c, err := ValidCredentials("prod", false); c != nil || err != nil {
			t.Fatalf("got %v %v", c, err)
		}
	})
	t.Run("issuer redirect keeps its own error", func(t *testing.T) {
		setHome(t)
		srv := httptest.NewServer(&fakeIssuer{redirect: true})
		defer srv.Close()
		saveCreds(t, srv.URL, 0)
		_, err := ValidCredentials("prod", false)
		var redirect *RedirectError
		if !errors.As(err, &redirect) {
			t.Fatalf("want RedirectError, got %v", err)
		}
	})
	t.Run("unreachable issuer is a network error", func(t *testing.T) {
		setHome(t)
		saveCreds(t, "http://127.0.0.1:1", 0)
		_, err := ValidCredentials("prod", false)
		if exits.Code(err) != exits.Network {
			t.Fatalf("want a Network exit, got %v", err)
		}
	})
	t.Run("local credentials are never refreshed", func(t *testing.T) {
		setHome(t)
		c := saveCreds(t, "local", 0)
		c.ClientID = "local"
		c.Save()
		if got, err := ValidCredentials("prod", true); err != nil || got.AccessToken != "old" {
			t.Fatalf("got %v %v", got, err)
		}
	})
}
