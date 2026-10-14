// Package api is the CLI's HTTP transport: auth headers, one transparent
// retry after a wrong-residency-cell refusal, and failures mapped to exit
// codes.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/residency"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/version"
)

// DefaultTimeout suits control-plane calls (/me, /namespaces). Calls with a
// model or a full-vault scan behind them pass their own.
const DefaultTimeout = 10 * time.Second

// Command is the subcommand being run, e.g. "memory search", sent as
// X-Kemory-Command. Names only, never arguments. Set by the CLI layer.
var Command string

// Notice receives the "switched cell" message after a residency retarget.
// The CLI points it at stderr.
var Notice = func(string) {}

// HTTPClient is used for every API call. It never follows redirects with the
// bearer token attached.
var HTTPClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Response is a fully read API response.
type Response struct {
	Status int
	Body   []byte
	Header http.Header
}

// JSON decodes the body into v.
func (r *Response) JSON(v any) error { return json.Unmarshal(r.Body, v) }

// RequireCreds returns valid credentials for env or an Auth error telling the
// user to log in.
func RequireCreds(env string) (*config.Credentials, error) {
	creds, err := auth.ValidCredentials(env, false)
	if err != nil {
		return nil, err
	}
	if creds == nil {
		return nil, exits.New("No valid credentials. Run `kemory login` first.", exits.Auth)
	}
	return creds, nil
}

// machine matches Python's platform.machine() names so server-side
// attribution stays continuous across the two CLIs.
func machine() string {
	switch runtime.GOARCH {
	case "amd64":
		if runtime.GOOS == "windows" {
			return "amd64"
		}
		return "x86_64"
	default:
		return runtime.GOARCH
	}
}

// ClientHeader is X-Kemory-Client: which client this is ("cli" for a typed
// command, "mcp-bridge" for `kemory mcp serve`), its version and platform.
func ClientHeader(kind string) string {
	return fmt.Sprintf("%s/%s (%s; %s)", kind, version.Version, runtime.GOOS, machine())
}

// Headers returns the auth, attribution and active-org headers.
func Headers(creds *config.Credentials) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+creds.AccessToken)
	h.Set("User-Agent", "kemory-cli/"+version.Version)
	h.Set("X-Kemory-Client", ClientHeader("cli"))
	if Command != "" {
		h.Set("X-Kemory-Command", Command)
	}
	if creds.ActiveOrgID != "" {
		h.Set("X-Organization-ID", creds.ActiveOrgID)
	}
	return h
}

func do(creds *config.Credentials, method, path string, body any, timeout time.Duration) (*Response, error) {
	var reader io.Reader
	if method != http.MethodGet && method != http.MethodDelete {
		if body == nil {
			body = map[string]any{}
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(creds.KemoryURL, "/")+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header = Headers(creds)
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		// The request never reached kemory, so nothing ran server-side.
		return nil, exits.New(fmt.Sprintf("Cannot reach %s (%v).", creds.KemoryURL, err), exits.Network)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, exits.New(fmt.Sprintf("Cannot reach %s (%v).", creds.KemoryURL, err), exits.Network)
	}
	return &Response{Status: resp.StatusCode, Body: data, Header: resp.Header}, nil
}

// Request issues a request and recovers once from a wrong-residency-cell 403.
// The refusal happens before any route body runs, so the retry is safe for
// any method. On recovery the corrected kemory_url is saved.
func Request(creds *config.Credentials, method, path string, body any, timeout time.Duration) (*Response, error) {
	return request(creds, method, path, body, timeout, false)
}

func request(creds *config.Credentials, method, path string, body any, timeout time.Duration, retried bool) (*Response, error) {
	resp, err := do(creds, method, path, body, timeout)
	if err != nil {
		return nil, err
	}
	refusal := residency.ParseRefusal(resp.Status, resp.Body)
	if refusal == nil {
		return resp, nil
	}
	if err := retarget(creds, refusal, retried); err != nil {
		return nil, err
	}
	return request(creds, method, path, body, timeout, true)
}

func retarget(creds *config.Credentials, refusal *residency.Refusal, retried bool) error {
	current := strings.TrimRight(creds.KemoryURL, "/")
	if retried {
		// A second refusal after a rewrite: explain and stop, never loop.
		return exits.New(residency.WrongRegionMessage(refusal, creds.KemoryURL), exits.Failure)
	}
	if !residency.IsAllowedAPIURL(current) {
		// Self-hosted or KEMORY_URL host: never rewrite what we did not set.
		return exits.New(residency.WrongRegionMessage(refusal, creds.KemoryURL)+
			"\nYour configured URL was left untouched.", exits.Failure)
	}
	target := resolveCellTarget(creds, refusal)
	if target == "" || target == current {
		return exits.New(residency.WrongRegionMessage(refusal, creds.KemoryURL), exits.Failure)
	}
	creds.KemoryURL = target
	if err := creds.Save(); err != nil {
		return err
	}
	Notice(fmt.Sprintf("→ Your organisation's data lives in the %s cell; switched this CLI to %s (saved).",
		residency.RegionFor(target), target))
	return nil
}

// resolveCellTarget returns the org's cell: the validated server target, or
// else the first other allowlisted cell that answers /api/v1/me with 200.
func resolveCellTarget(creds *config.Credentials, refusal *residency.Refusal) string {
	for _, candidate := range residency.CandidateHosts(refusal, creds.KemoryURL) {
		if candidate == refusal.TargetAPI {
			return candidate
		}
		probe := *creds
		probe.KemoryURL = candidate
		resp, err := do(&probe, http.MethodGet, "/api/v1/me", nil, DefaultTimeout)
		if err == nil && resp.Status == http.StatusOK {
			return candidate
		}
	}
	return ""
}

// Get is Request with GET.
func Get(creds *config.Credentials, path string) (*Response, error) {
	return Request(creds, http.MethodGet, path, nil, DefaultTimeout)
}

// Fail returns an error carrying the server's own words and the exit code its
// status implies.
func Fail(resp *Response, what string) error {
	return exits.New(fmt.Sprintf("%s failed: %d %s", what, resp.Status, resp.Body), exits.ForStatus(resp.Status))
}

// Post is Request with POST and a JSON body.
func Post(creds *config.Credentials, path string, body any, timeout time.Duration) (*Response, error) {
	return Request(creds, http.MethodPost, path, body, timeout)
}

// Delete is Request with DELETE.
func Delete(creds *config.Credentials, path string) (*Response, error) {
	return Request(creds, http.MethodDelete, path, nil, DefaultTimeout)
}

// StreamTo copies a GET response body into w without buffering it, and
// returns the number of bytes written. There is no residency retry: a refusal
// arrives before any body, and the next ordinary command fixes the cell.
func StreamTo(creds *config.Credentials, path string, w io.Writer, timeout time.Duration) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(creds.KemoryURL, "/")+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header = Headers(creds)
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return 0, exits.New(fmt.Sprintf("Cannot reach %s (%v).", creds.KemoryURL, err), exits.Network)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return 0, Fail(&Response{Status: resp.StatusCode, Body: body}, "GET "+path)
	}
	n, err := io.Copy(w, resp.Body)
	if err != nil {
		// A stream can die mid-body; the partial output must read as a failure.
		return n, exits.New(fmt.Sprintf("Cannot reach %s (%v). The output is incomplete.", creds.KemoryURL, err), exits.Network)
	}
	return n, nil
}
