// Package auth implements the OAuth 2.0 device-authorization flow (RFC 8628)
// with PKCE, token refresh, and loading credentials that are still valid.
//
// Device flow suits a CLI that may run over SSH or in a container: the user
// approves in any browser and the CLI only polls the token endpoint.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
)

// DefaultScope is requested on every login.
const DefaultScope = "openid profile email offline_access"

// HTTPClient is used for every issuer call. It never follows redirects: a
// credential-bearing POST must not be replayed to a host named by a response.
var HTTPClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Sleep is the poll delay. Overridable in tests.
var Sleep = time.Sleep

// FlowError is a device-flow or refresh failure.
type FlowError struct{ Message string }

func (e *FlowError) Error() string { return e.Message }

// RedirectError means the issuer answered a credential POST with a redirect.
// That is a stale issuer URL, not an expired session: re-running `kemory login`
// fails the same way, so callers must not turn it into "please log in".
type RedirectError struct {
	What     string
	Status   int
	Location string
}

func (e *RedirectError) Error() string {
	target := e.Location
	if target == "" {
		target = "an undisclosed host (no Location header)"
	}
	return fmt.Sprintf("%s: the issuer host redirected (%d) to %s. "+
		"The CLI does not replay credential-bearing POSTs across a redirect. "+
		"This is a stale issuer URL, NOT an expired session — re-running "+
		"`kemory login` will fail the same way. Point the issuer at the "+
		"host above (KEMORY_OIDC_ISSUER overrides it) or upgrade the CLI.",
		e.What, e.Status, target)
}

// DeviceCode is the issuer's answer to a device-authorization request.
type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// Token is a token-endpoint response.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// PKCEPair returns an S256 (verifier, challenge) pair.
func PKCEPair() (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func endpoint(issuer, path string) string {
	return strings.TrimRight(issuer, "/") + "/protocol/openid-connect/" + path
}

// reply is a fully read issuer response.
type reply struct {
	StatusCode int
	Location   string
}

func postForm(target string, form url.Values) (*reply, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), HTTPClient.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return &reply{StatusCode: resp.StatusCode, Location: resp.Header.Get("Location")}, body, err
}

func checkRedirect(what string, resp *reply) error {
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return &RedirectError{What: what, Status: resp.StatusCode, Location: resp.Location}
	}
	return nil
}

// RequestDeviceCode starts the device flow. When challenge is set, the matching
// verifier must be passed to PollForToken.
func RequestDeviceCode(issuer, clientID, challenge string) (*DeviceCode, error) {
	form := url.Values{"client_id": {clientID}, "scope": {DefaultScope}}
	if challenge != "" {
		form.Set("code_challenge", challenge)
		form.Set("code_challenge_method", "S256")
	}
	resp, body, err := postForm(endpoint(issuer, "auth/device"), form)
	if err != nil {
		return nil, &FlowError{Message: fmt.Sprintf("device_code request failed: %v", err)}
	}
	if err := checkRedirect("device_code request failed", resp); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &FlowError{Message: fmt.Sprintf("device_code request failed: %d %s", resp.StatusCode, body)}
	}
	var dc DeviceCode
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, &FlowError{Message: fmt.Sprintf("device_code request returned invalid JSON: %v", err)}
	}
	if dc.VerificationURIComplete == "" {
		dc.VerificationURIComplete = dc.VerificationURI
	}
	if dc.ExpiresIn == 0 {
		dc.ExpiresIn = 600
	}
	if dc.Interval == 0 {
		dc.Interval = 5
	}
	return &dc, nil
}

// PollForToken blocks until the user approves or the device code expires.
func PollForToken(issuer, clientID string, dc *DeviceCode, verifier string) (*Token, error) {
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	interval := time.Duration(dc.Interval) * time.Second
	for time.Now().Before(deadline) {
		form := url.Values{
			"client_id":   {clientID},
			"device_code": {dc.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}
		if verifier != "" {
			form.Set("code_verifier", verifier)
		}
		resp, body, err := postForm(endpoint(issuer, "token"), form)
		if err != nil {
			return nil, &FlowError{Message: fmt.Sprintf("device flow token request failed: %v", err)}
		}
		if resp.StatusCode == http.StatusOK {
			var tok Token
			if err := json.Unmarshal(body, &tok); err != nil {
				return nil, &FlowError{Message: fmt.Sprintf("token response is not JSON: %v", err)}
			}
			return &tok, nil
		}
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &e)
		switch e.Error {
		case "authorization_pending":
			Sleep(interval)
			continue
		case "slow_down":
			interval += 5 * time.Second
			Sleep(interval)
			continue
		case "expired_token", "access_denied":
			return nil, &FlowError{Message: fmt.Sprintf("device flow %s: %s", e.Error, e.Description)}
		}
		return nil, &FlowError{Message: fmt.Sprintf("device flow unexpected response: %d %s", resp.StatusCode, body)}
	}
	return nil, &FlowError{Message: "device code expired before approval"}
}

// isHeadless reports environments where opening a browser would be useless.
func isHeadless() bool {
	if os.Getenv("KEMORY_HEADLESS") == "1" {
		return true
	}
	if runtime.GOOS == "linux" {
		return os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == ""
	}
	return os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != ""
}

// OpenBrowser is how the verification URL is opened. Overridable in tests.
var OpenBrowser = openBrowser

// Login runs the device flow with PKCE and saves the resulting credentials.
// Messages for the user go to out.
func Login(issuer, clientID, kemoryURL, env string, openBrowser bool, out io.Writer) (*config.Credentials, error) {
	return LoginWith(issuer, clientID, kemoryURL, env, openBrowser, LoginUI{
		ShowCode: func(uri, code string) {
			fmt.Fprintf(out, "\nOpen this URL in any browser and enter the code:\n  %s\n  Code: %s\n\n", uri, code)
		},
		Wait: func(poll func()) {
			fmt.Fprintln(out, "Waiting for approval... (Ctrl-C to cancel)")
			poll()
		},
	})
}

// LoginUI is how the device flow talks to the user: ShowCode gives the URL
// and code to enter, and Wait runs poll, which returns once the user has
// approved, denied or let the code expire.
type LoginUI struct {
	ShowCode func(uri, code string)
	Wait     func(poll func())
}

// LoginWith is Login with its messages drawn by ui.
func LoginWith(issuer, clientID, kemoryURL, env string, openBrowser bool, ui LoginUI) (*config.Credentials, error) {
	verifier, challenge, err := PKCEPair()
	if err != nil {
		return nil, err
	}
	dc, err := RequestDeviceCode(issuer, clientID, challenge)
	if err != nil {
		return nil, err
	}
	ui.ShowCode(dc.VerificationURI, dc.UserCode)
	if openBrowser && !isHeadless() {
		_ = OpenBrowser(dc.VerificationURIComplete)
	}
	var tok *Token
	ui.Wait(func() { tok, err = PollForToken(issuer, clientID, dc, verifier) })
	if err != nil {
		return nil, err
	}
	creds := &config.Credentials{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    expiry(tok.ExpiresIn),
		Issuer:       issuer,
		ClientID:     clientID,
		KemoryURL:    kemoryURL,
		Env:          env,
	}
	if err := creds.Save(); err != nil {
		return nil, err
	}
	return creds, nil
}

func expiry(expiresIn int) float64 {
	if expiresIn == 0 {
		expiresIn = 300
	}
	return float64(time.Now().Add(time.Duration(expiresIn)*time.Second).UnixMicro()) / 1e6
}

// Refresh exchanges the refresh token for a new access token and saves it.
func Refresh(creds *config.Credentials) error {
	form := url.Values{
		"client_id":     {creds.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
	}
	resp, body, err := postForm(endpoint(creds.Issuer, "token"), form)
	if err != nil {
		// The issuer was never reached; the session may be fine.
		return exits.New(fmt.Sprintf("Cannot reach %s (%v).", creds.Issuer, err), exits.Network)
	}
	if err := checkRedirect("refresh failed", resp); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return &FlowError{Message: fmt.Sprintf("refresh failed: %d %s. Run `kemory login`.", resp.StatusCode, body)}
	}
	var tok Token
	if err := json.Unmarshal(body, &tok); err != nil {
		return &FlowError{Message: fmt.Sprintf("refresh response is not JSON: %v", err)}
	}
	creds.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		creds.RefreshToken = tok.RefreshToken
	}
	creds.ExpiresAt = expiry(tok.ExpiresIn)
	return creds.Save()
}

// ValidCredentials loads the credentials for env, refreshing them when they
// expire within a minute (or always, with force). It returns (nil, nil) when
// the user must log in: no file, or the issuer rejected the refresh token.
// A RedirectError or an unreachable issuer is returned as an error, because
// logging in cannot fix either.
func ValidCredentials(env string, force bool) (*config.Credentials, error) {
	creds, err := config.LoadCredentials(env)
	if err != nil || creds == nil {
		return nil, err
	}
	if creds.IsLocal() {
		return creds, nil
	}
	if force || creds.ExpiresWithin(60*time.Second) {
		if err := Refresh(creds); err != nil {
			// Only a genuine rejection means "log in again". A redirect (stale
			// issuer) or an unreachable issuer must keep its own message.
			var flow *FlowError
			if errors.As(err, &flow) {
				return nil, nil
			}
			return nil, err
		}
	}
	return creds, nil
}
