package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
)

// The CLI, the MCP bridge and the plugin's hooks all refresh the same
// credentials file. When another process wins the refresh and the issuer
// rotates refresh tokens, this process's refresh with the old token is
// rejected, yet the file on disk already holds a fresh session. That must not
// be reported as "not logged in".
func TestRefreshRejectedAfterAnotherProcessRefreshedUsesTheSavedSession(t *testing.T) {
	setHome(t)
	var issuer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("refresh_token") == "rt-old" {
			// Another process refreshed first and saved the rotated tokens.
			other := &config.Credentials{
				AccessToken: "from-other-process", RefreshToken: "rt-new",
				ExpiresAt: float64(time.Now().Add(time.Hour).Unix()),
				Issuer:    issuer, ClientID: "kemory-cli", KemoryURL: "https://k", Env: "prod",
			}
			if err := other.Save(); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error": "invalid_grant", "error_description": "Stale token"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token": "refreshed", "refresh_token": "rt-newer", "expires_in": 300}`))
	}))
	defer srv.Close()
	issuer = srv.URL
	saveCreds(t, srv.URL, 0)

	c, err := ValidCredentials("prod", false)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		saved, _ := config.LoadCredentials("prod")
		t.Fatalf("ValidCredentials returned nil (\"not logged in\") while the file holds a valid session (access token %q)", saved.AccessToken)
	}
}

func rejectingIssuer(t *testing.T, onReject func()) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onReject != nil {
			onReject()
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func saveRotated(t *testing.T, issuer string, expiresIn time.Duration) {
	t.Helper()
	c := &config.Credentials{
		AccessToken: "from-other-process", RefreshToken: "rt-new",
		ExpiresAt: float64(time.Now().Add(expiresIn).Unix()),
		Issuer:    issuer, ClientID: "kemory-cli", KemoryURL: "https://k", Env: "prod",
	}
	if err := c.Save(); err != nil {
		t.Error(err)
	}
}

func TestRefreshRejectedThenOtherProcessSavesUsesTheSavedSession(t *testing.T) {
	setHome(t)
	srv := rejectingIssuer(t, nil)
	saveCreds(t, srv.URL, 0)

	calls := 0
	old := Sleep
	Sleep = func(time.Duration) {
		calls++
		if calls == 1 {
			saveRotated(t, srv.URL, time.Hour)
		}
	}
	t.Cleanup(func() { Sleep = old })

	c, err := ValidCredentials("prod", false)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.AccessToken != "from-other-process" {
		t.Fatalf("got %+v, want the session saved during the wait", c)
	}
	if calls != 1 {
		t.Fatalf("Sleep called %d times, want 1", calls)
	}
}

func TestRefreshRejectedWithUnchangedFileMeansLogIn(t *testing.T) {
	setHome(t)
	saved := noSleep(t)
	srv := rejectingIssuer(t, nil)
	saveCreds(t, srv.URL, 0)

	c, err := ValidCredentials("prod", false)
	if c != nil || err != nil {
		t.Fatalf("got %v %v, want nil nil", c, err)
	}
	if len(*saved) != 5 {
		t.Fatalf("Sleep called %d times, want 5", len(*saved))
	}
	for _, d := range *saved {
		if d != 200*time.Millisecond {
			t.Fatalf("Sleep(%v), want 200ms", d)
		}
	}
}

func TestRefreshRejectedWhenSavedSessionAlsoExpiredMeansLogIn(t *testing.T) {
	setHome(t)
	saved := noSleep(t)
	var issuer string
	srv := rejectingIssuer(t, func() { saveRotated(t, issuer, 0) })
	issuer = srv.URL
	saveCreds(t, srv.URL, 0)

	c, err := ValidCredentials("prod", false)
	if c != nil || err != nil {
		t.Fatalf("got %v %v, want nil nil", c, err)
	}
	if len(*saved) != 0 {
		t.Fatalf("Sleep called %d times, want 0", len(*saved))
	}
}
