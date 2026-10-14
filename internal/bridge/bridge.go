// Package bridge is `kemory mcp serve`: a stdio MCP server that MCP hosts
// launch, which relays every request to Kemory's Streamable HTTP endpoint
// ($KEMORY_URL/mcp/v1) with the user's credentials attached.
//
// It is a transparent relay rather than an SDK server: upstream results are
// passed through as raw JSON, so fields this build does not know about
// (titles, annotations, output schemas, structured content) reach the host
// unchanged.
//
// stdout carries only JSON-RPC messages. Everything else goes to stderr.
package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/residency"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/version"
)

// supportedVersions are the MCP protocol versions this bridge speaks, newest
// first. The host's requested version is echoed when supported.
var supportedVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// maxHandshakeAttempts caps upstream `initialize` retries, so a server that
// refuses the handshake is not re-asked on every call.
const maxHandshakeAttempts = 3

// HTTPClient is used for every upstream call. Overridable in tests. It never
// follows redirects with credentials attached.
var HTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:       http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Logf writes a diagnostic line to stderr. Overridable in tests.
var Logf = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "kemory: "+format+"\n", args...)
}

// Bridge holds one host connection's state.
type Bridge struct {
	env string

	out   io.Writer
	outMu sync.Mutex

	// credMu serialises credential loading: two concurrent requests must not
	// both refresh, because the issuer may rotate the refresh token.
	credMu sync.Mutex

	stateMu           sync.Mutex
	hostName          string
	hostVersion       string
	handshakeDone     bool
	handshakeAttempts int
	discoveryDone     bool
	legacyKeyWarned   bool
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("%d: %s", e.Code, e.Message) }

const (
	codeParseError     = -32700
	codeMethodNotFound = -32601
	codeInternal       = -32603
)

// Serve runs the bridge until in is closed. It returns nil on a clean EOF.
func Serve(env string, in io.Reader, out io.Writer) error {
	b := &Bridge{env: env, out: out}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	var wg sync.WaitGroup
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg message
		if err := json.Unmarshal(line, &msg); err != nil {
			b.write(message{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: codeParseError, Message: "Parse error"}})
			continue
		}
		if len(msg.ID) == 0 || msg.Method == "" {
			continue // a notification, or a response to a request we never send
		}
		if msg.Method == "initialize" {
			// Handled inline: nothing else may run before it completes.
			b.handle(msg)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.handle(msg)
		}()
	}
	wg.Wait()
	return scanner.Err()
}

func (b *Bridge) write(msg message) {
	data, err := json.Marshal(msg)
	if err != nil {
		Logf("could not encode a response: %v", err)
		return
	}
	b.outMu.Lock()
	defer b.outMu.Unlock()
	_, _ = b.out.Write(append(data, '\n'))
}

func (b *Bridge) reply(id json.RawMessage, result any) {
	raw, err := json.Marshal(result)
	if err != nil {
		b.fail(id, &rpcError{Code: codeInternal, Message: err.Error()})
		return
	}
	b.write(message{JSONRPC: "2.0", ID: id, Result: raw})
}

func (b *Bridge) fail(id json.RawMessage, e *rpcError) {
	b.write(message{JSONRPC: "2.0", ID: id, Error: e})
}

func (b *Bridge) handle(msg message) {
	switch msg.Method {
	case "initialize":
		b.reply(msg.ID, b.initialize(msg.Params))
	case "ping":
		b.reply(msg.ID, map[string]any{})
	case "tools/list":
		b.reply(msg.ID, b.listTools(msg.Params))
	case "tools/call":
		b.reply(msg.ID, b.callTool(msg.Params))
	case "prompts/list":
		b.relayList(msg, "prompts")
	case "resources/list":
		b.relayList(msg, "resources")
	case "resources/templates/list":
		b.relayList(msg, "resourceTemplates")
	case "prompts/get", "resources/read":
		b.relay(msg)
	case "completion/complete":
		result, err := b.forward(msg.Method, msg.Params)
		if err != nil {
			b.reply(msg.ID, map[string]any{"completion": map[string]any{"values": []string{}}})
			return
		}
		b.write(message{JSONRPC: "2.0", ID: msg.ID, Result: result})
	default:
		b.fail(msg.ID, &rpcError{Code: codeMethodNotFound, Message: "Method not found: " + msg.Method})
	}
}

// --- initialize ---------------------------------------------------------------

func (b *Bridge) initialize(params json.RawMessage) map[string]any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
		ClientInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}
	_ = json.Unmarshal(params, &p)
	b.stateMu.Lock()
	b.hostName = strings.TrimSpace(p.ClientInfo.Name)
	b.hostVersion = p.ClientInfo.Version
	b.stateMu.Unlock()

	protocol := supportedVersions[0]
	for _, v := range supportedVersions {
		if v == p.ProtocolVersion {
			protocol = v
		}
	}
	result := map[string]any{
		"protocolVersion": protocol,
		"capabilities": map[string]any{
			"tools":       map[string]any{},
			"prompts":     map[string]any{},
			"resources":   map[string]any{},
			"completions": map[string]any{},
		},
		"serverInfo": map[string]any{"name": "kemory", "version": version.Version},
	}
	// The server's instructions are the authoritative text; the bridge only
	// relays them. Without an upstream answer the host still gets a working
	// session, just without instructions.
	if instructions := b.handshake(protocol); instructions != "" {
		result["instructions"] = instructions
	}
	return result
}

// handshake sends `initialize` upstream, which is what registers the host as
// an agent server-side, and returns the server's instructions. It never fails
// a request: a refused handshake is logged and retried on later calls.
func (b *Bridge) handshake(protocol string) string {
	b.stateMu.Lock()
	if b.handshakeDone || b.handshakeAttempts >= maxHandshakeAttempts {
		b.stateMu.Unlock()
		return ""
	}
	b.handshakeAttempts++
	name, ver := b.hostName, b.hostVersion
	b.stateMu.Unlock()

	if name == "" {
		name, ver = "kemory-cli", version.Version
	}
	if protocol == "" {
		protocol = supportedVersions[0]
	}
	b.ensureHomeCell()
	params, _ := json.Marshal(map[string]any{
		"protocolVersion": protocol,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": name, "version": ver},
	})
	result, err := b.call("initialize", params, 15*time.Second)
	if err != nil {
		Logf("upstream initialize failed (%v); continuing without registration", err)
		return ""
	}
	b.stateMu.Lock()
	b.handshakeDone = true
	b.stateMu.Unlock()
	var r struct {
		Instructions string `json:"instructions"`
	}
	_ = json.Unmarshal(result, &r)
	return r.Instructions
}

// --- upstream transport -------------------------------------------------------

// upstreamError is an HTTP-level failure with its status and body.
type upstreamError struct {
	Status int
	Body   []byte
}

func (e *upstreamError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

// resolveURL is KEMORY_URL, else the cached kemory_url, else local dev.
func (b *Bridge) resolveURL() string {
	if u := os.Getenv("KEMORY_URL"); u != "" {
		return u
	}
	if creds, _ := config.LoadCredentials(b.env); creds != nil && creds.KemoryURL != "" {
		return creds.KemoryURL
	}
	return "http://localhost:8100"
}

func (b *Bridge) userAgent() string {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	if b.hostName == "" {
		return "kemory-cli/" + version.Version
	}
	if b.hostVersion == "" {
		return b.hostName
	}
	return b.hostName + "/" + b.hostVersion
}

// headers picks the auth: KEMORY_API_KEY (or a legacy alias) as X-API-Key,
// else the cached OAuth token as a bearer plus the selected org.
func (b *Bridge) headers() http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json, text/event-stream")
	h.Set("User-Agent", b.userAgent())
	h.Set("X-Kemory-Client", api.ClientHeader("mcp-bridge"))

	key := os.Getenv("KEMORY_API_KEY")
	if key == "" {
		if key = firstNonEmpty(os.Getenv("S9NMV_API_KEY"), os.Getenv("KORA_API_KEY")); key != "" {
			b.stateMu.Lock()
			if !b.legacyKeyWarned {
				b.legacyKeyWarned = true
				Logf("using a deprecated API key variable (S9NMV_API_KEY / KORA_API_KEY); switch to KEMORY_API_KEY")
			}
			b.stateMu.Unlock()
		}
	}
	if key != "" {
		h.Set("X-API-Key", key)
		return h
	}
	b.credMu.Lock()
	creds, err := auth.ValidCredentials(b.env, false)
	b.credMu.Unlock()
	if err != nil {
		// A stale issuer or an unreachable one: the call proceeds
		// unauthenticated and the server's answer says what is wrong.
		Logf("auth unavailable, proceeding unauthenticated: %v", err)
		return h
	}
	if creds != nil {
		h.Set("Authorization", "Bearer "+creds.AccessToken)
		if creds.ActiveOrgID != "" {
			h.Set("X-Organization-ID", creds.ActiveOrgID)
		}
	}
	return h
}

func readTimeout() time.Duration {
	if v, err := strconv.ParseFloat(os.Getenv("KEMORY_HTTP_TIMEOUT"), 64); err == nil && v > 0 {
		return time.Duration(v * float64(time.Second))
	}
	// Writes embed and index before the server answers; a short timeout
	// reports failure for a write that actually succeeded.
	return 120 * time.Second
}

// call POSTs one JSON-RPC request to /mcp/v1 and returns its result. It
// returns *upstreamError for HTTP failures and *rpcError for a JSON-RPC error.
func (b *Bridge) call(method string, params json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if len(params) > 0 && string(params) != "null" {
		payload["params"] = params
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(b.resolveURL(), "/")+"/mcp/v1", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = b.headers()
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &upstreamError{Status: resp.StatusCode, Body: data}
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		data = lastSSEData(data)
	}
	var reply message
	if err := json.Unmarshal(data, &reply); err != nil {
		return nil, fmt.Errorf("upstream returned invalid JSON: %w", err)
	}
	if reply.Error != nil {
		return nil, reply.Error
	}
	if len(reply.Result) == 0 {
		return json.RawMessage("{}"), nil
	}
	return reply.Result, nil
}

// lastSSEData returns the last `data:` payload of an event stream, which for
// a single request is its response.
func lastSSEData(stream []byte) []byte {
	var last []byte
	for _, line := range bytes.Split(stream, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if rest, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			last = bytes.TrimSpace(rest)
		}
	}
	return last
}

// --- residency ----------------------------------------------------------------

// ensureHomeCell asks once per process where the org's data lives, so the
// first call already goes to the right cell. Every failure means "stay put";
// the 403 recovery still covers a wrong-cell call.
func (b *Bridge) ensureHomeCell() {
	b.stateMu.Lock()
	if b.discoveryDone {
		b.stateMu.Unlock()
		return
	}
	b.discoveryDone = true
	b.stateMu.Unlock()
	if os.Getenv("KEMORY_URL") != "" {
		return // an explicit operator override is never fought
	}
	creds, _ := config.LoadCredentials(b.env)
	if creds == nil || !residency.IsAllowedAPIURL(creds.KemoryURL) {
		return
	}
	// Discovery answers for the token's org; with another org selected its
	// answer would route to the wrong cell.
	if residency.SelectionOverridesDiscovery(creds.ActiveOrgID, creds.OrgID) {
		return
	}
	if home := residency.DiscoverHomeCell(creds.KemoryURL, b.headers()); home != "" {
		b.saveURL(home, "discovery")
	}
}

// recoverResidency points the cached kemory_url at the org's cell after a
// wrong-cell refusal and returns the new URL, or "" when it must not or
// cannot: KEMORY_URL is set, the cached URL is not a known cell (self-hosted),
// or no candidate cell answers for this identity.
func (b *Bridge) recoverResidency(refusal *residency.Refusal) string {
	if os.Getenv("KEMORY_URL") != "" {
		return ""
	}
	creds, _ := config.LoadCredentials(b.env)
	if creds == nil || !residency.IsAllowedAPIURL(creds.KemoryURL) {
		return ""
	}
	headers := b.headers()
	for _, candidate := range residency.CandidateHosts(refusal, creds.KemoryURL) {
		if candidate != refusal.TargetAPI && !b.answersMe(candidate, headers) {
			continue
		}
		b.saveURL(candidate, "403 recovery")
		return candidate
	}
	return ""
}

func (b *Bridge) answersMe(host string, headers http.Header) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, host+"/api/v1/me", nil)
	if err != nil {
		return false
	}
	req.Header = headers.Clone()
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (b *Bridge) saveURL(url, why string) {
	b.credMu.Lock()
	defer b.credMu.Unlock()
	creds, _ := config.LoadCredentials(b.env)
	if creds == nil {
		return
	}
	creds.KemoryURL = url
	if err := creds.Save(); err != nil {
		Logf("residency: could not save %s: %v", url, err)
		return
	}
	Logf("residency: %s re-targeted kemory_url to %s", why, url)
}

func refusalOf(err error) *residency.Refusal {
	var up *upstreamError
	if errors.As(err, &up) {
		return residency.ParseRefusal(up.Status, up.Body)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
