package bridge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/residency"
)

func setup(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	for _, k := range []string{"KEMORY_URL", "KEMORY_API_KEY", "S9NMV_API_KEY", "KORA_API_KEY", "KEMORY_HTTP_TIMEOUT"} {
		t.Setenv(k, "")
	}
	old := Logf
	Logf = func(string, ...any) {}
	t.Cleanup(func() { Logf = old })
}

// upstream is a fake Kemory /mcp/v1 endpoint.
type upstream struct {
	mu    sync.Mutex
	calls []upstreamCall
	reply func(method string, params json.RawMessage, h http.Header) (int, string)
}

type upstreamCall struct {
	Method string
	Params json.RawMessage
	Header http.Header
}

func (u *upstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/mcp/v1":
	case "/api/v1/residency/cell":
		w.Write([]byte(`{"is_home_cell": true}`))
		return
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	u.mu.Lock()
	u.calls = append(u.calls, upstreamCall{req.Method, req.Params, r.Header.Clone()})
	u.mu.Unlock()
	status, body := u.reply(req.Method, req.Params, r.Header)
	if strings.HasPrefix(body, "event:") {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	w.Write([]byte(body))
}

func (u *upstream) callsTo(method string) []upstreamCall {
	u.mu.Lock()
	defer u.mu.Unlock()
	var out []upstreamCall
	for _, c := range u.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func ok(result string) (int, string) {
	return 200, `{"jsonrpc": "2.0", "id": 1, "result": ` + result + `}`
}

const instructions = "Check the vault first."

func defaultReply(method string, _ json.RawMessage, _ http.Header) (int, string) {
	switch method {
	case "initialize":
		return ok(`{"protocolVersion": "2025-06-18", "capabilities": {}, "serverInfo": {"name": "kemory"}, "instructions": "` + instructions + `"}`)
	case "tools/list":
		return ok(`{"tools": [{"name": "kemory_recall_memory", "title": "Recall", "description": "d", "inputSchema": {"type": "object"},
			"annotations": {"readOnlyHint": true}, "outputSchema": {"type": "object"}}]}`)
	case "tools/call":
		return ok(`{"content": [{"type": "text", "text": "found 2"}], "structuredContent": {"count": 2}}`)
	case "prompts/list":
		return ok(`{"prompts": [{"name": "enhance"}]}`)
	case "prompts/get":
		return 200, `{"jsonrpc": "2.0", "id": 1, "error": {"code": -32602, "message": "unknown prompt"}}`
	}
	return ok(`{}`)
}

func newUpstream(t *testing.T, reply func(string, json.RawMessage, http.Header) (int, string)) (*upstream, *httptest.Server) {
	t.Helper()
	if reply == nil {
		reply = defaultReply
	}
	u := &upstream{reply: reply}
	srv := httptest.NewServer(u)
	t.Cleanup(srv.Close)
	return u, srv
}

func saveCreds(t *testing.T, kemoryURL, issuer string, expiresIn time.Duration) {
	t.Helper()
	c := &config.Credentials{
		AccessToken: "at", RefreshToken: "rt", ExpiresAt: float64(time.Now().Add(expiresIn).Unix()),
		Issuer: issuer, ClientID: "kemory-cli", KemoryURL: kemoryURL, Env: "prod", ActiveOrgID: "org-9",
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
}

// session runs the bridge over the given requests and returns the responses
// by id, failing on any stdout line that is not a JSON-RPC message.
func session(t *testing.T, requests ...string) map[string]map[string]any {
	t.Helper()
	var out strings.Builder
	if err := Serve("prod", strings.NewReader(strings.Join(requests, "\n")+"\n"), &syncWriter{w: &out}); err != nil {
		t.Fatal(err)
	}
	responses := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil || m["jsonrpc"] != "2.0" {
			t.Fatalf("stdout carried a non-JSON-RPC line: %q", line)
		}
		id, _ := json.Marshal(m["id"])
		responses[string(id)] = m
	}
	return responses
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// Messages on stdio are newline-delimited, so every request is one line.
const initReq = `{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "claude-desktop", "version": "1.2"}}}`

func result(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	r, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", resp)
	}
	return r
}

func TestInitializeRelaysTheServerInstructionsAndRegistersTheHost(t *testing.T) {
	setup(t)
	u, srv := newUpstream(t, nil)
	saveCreds(t, srv.URL, "https://unused.invalid", time.Hour)
	resp := session(t, initReq, `{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}`)

	init := result(t, resp["1"])
	if init["instructions"] != instructions || init["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize: %v", init)
	}
	if info := init["serverInfo"].(map[string]any); info["name"] != "kemory" {
		t.Fatalf("serverInfo: %v", info)
	}
	ups := u.callsTo("initialize")
	if len(ups) != 1 {
		t.Fatalf("want exactly one upstream initialize, got %d", len(ups))
	}
	if !strings.Contains(string(ups[0].Params), `"name":"claude-desktop"`) || ups[0].Header.Get("Authorization") != "Bearer at" {
		t.Fatalf("upstream initialize must carry the host and the token: %s %v", ups[0].Params, ups[0].Header)
	}
	list := u.callsTo("tools/list")[0]
	if list.Header.Get("User-Agent") != "claude-desktop/1.2" || list.Header.Get("X-Organization-ID") != "org-9" ||
		!strings.HasPrefix(list.Header.Get("X-Kemory-Client"), "mcp-bridge/") {
		t.Fatalf("tools/list headers: %v", list.Header)
	}
}

func TestUnsupportedProtocolVersionGetsTheNewest(t *testing.T) {
	setup(t)
	_, srv := newUpstream(t, nil)
	saveCreds(t, srv.URL, "https://unused.invalid", time.Hour)
	resp := session(t, `{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "1999-01-01"}}`)
	if v := result(t, resp["1"])["protocolVersion"]; v != supportedVersions[0] {
		t.Fatalf("got %v", v)
	}
}

func TestToolsArePassedThroughWithoutLosingFields(t *testing.T) {
	setup(t)
	u, srv := newUpstream(t, nil)
	saveCreds(t, srv.URL, "https://unused.invalid", time.Hour)
	resp := session(t, initReq,
		`{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}`,
		`{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "kemory_recall_memory", "_meta": {"progressToken": 7}}}`)

	tool := result(t, resp["2"])["tools"].([]any)[0].(map[string]any)
	if tool["title"] != "Recall" || tool["annotations"] == nil || tool["outputSchema"] == nil {
		t.Fatalf("fields dropped: %v", tool)
	}
	call := result(t, resp["3"])
	if call["structuredContent"] == nil || call["content"].([]any)[0].(map[string]any)["text"] != "found 2" {
		t.Fatalf("call result: %v", call)
	}
	sent := string(u.callsTo("tools/call")[0].Params)
	if !strings.Contains(sent, `"arguments":{}`) || strings.Contains(sent, "_meta") {
		t.Fatalf("forwarded params: %s", sent)
	}
}

func TestPromptsResourcesAndCompletionRelay(t *testing.T) {
	setup(t)
	_, srv := newUpstream(t, func(method string, p json.RawMessage, h http.Header) (int, string) {
		if method == "resources/list" || method == "completion/complete" {
			return 500, "boom"
		}
		return defaultReply(method, p, h)
	})
	saveCreds(t, srv.URL, "https://unused.invalid", time.Hour)
	resp := session(t, initReq,
		`{"jsonrpc": "2.0", "id": 2, "method": "prompts/list"}`,
		`{"jsonrpc": "2.0", "id": 3, "method": "prompts/get", "params": {"name": "nope"}}`,
		`{"jsonrpc": "2.0", "id": 4, "method": "resources/list"}`,
		`{"jsonrpc": "2.0", "id": 5, "method": "completion/complete", "params": {}}`)

	if p := result(t, resp["2"])["prompts"].([]any); len(p) != 1 {
		t.Fatalf("prompts: %v", p)
	}
	e := resp["3"]["error"].(map[string]any)
	if e["code"].(float64) != -32602 || e["message"] != "unknown prompt" {
		t.Fatalf("the server's own error must be relayed: %v", e)
	}
	if r := result(t, resp["4"])["resources"].([]any); len(r) != 0 {
		t.Fatalf("a failed list must be empty: %v", r)
	}
	if v := result(t, resp["5"])["completion"].(map[string]any)["values"].([]any); len(v) != 0 {
		t.Fatalf("a failed completion must be empty: %v", v)
	}
}

func TestProtocolBasics(t *testing.T) {
	setup(t)
	_, srv := newUpstream(t, nil)
	saveCreds(t, srv.URL, "https://unused.invalid", time.Hour)
	resp := session(t, initReq,
		`{"jsonrpc": "2.0", "method": "notifications/initialized"}`,
		`{"jsonrpc": "2.0", "id": "p", "method": "ping"}`,
		`{"jsonrpc": "2.0", "id": 9, "method": "sampling/whatever"}`,
		`{not json`)
	if len(resp) != 4 {
		t.Fatalf("a notification must get no response; got %d responses: %v", len(resp), resp)
	}
	if _, ok := resp[`"p"`]["result"]; !ok {
		t.Fatalf("ping: %v", resp[`"p"`])
	}
	if resp["9"]["error"].(map[string]any)["code"].(float64) != codeMethodNotFound {
		t.Fatalf("unknown method: %v", resp["9"])
	}
	if resp["null"]["error"].(map[string]any)["code"].(float64) != codeParseError {
		t.Fatalf("bad JSON: %v", resp["null"])
	}
}

func TestWithoutCredentialsAToolCallSaysToLogIn(t *testing.T) {
	setup(t)
	u, srv := newUpstream(t, nil)
	t.Setenv("KEMORY_URL", srv.URL)
	resp := session(t, `{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "x"}}`)
	call := result(t, resp["3"])
	if call["isError"] != true || !strings.Contains(call["content"].([]any)[0].(map[string]any)["text"].(string), "kemory login") {
		t.Fatalf("got %v", call)
	}
	if len(u.callsTo("tools/call")) != 0 {
		t.Fatal("an unauthenticated tool call must not reach the server")
	}
}

func TestAPIKeyIsSentInsteadOfTheToken(t *testing.T) {
	setup(t)
	u, srv := newUpstream(t, nil)
	saveCreds(t, srv.URL, "https://unused.invalid", time.Hour)
	t.Setenv("KEMORY_API_KEY", "key-1")
	session(t, `{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "x"}}`)
	h := u.callsTo("tools/call")[0].Header
	if h.Get("X-API-Key") != "key-1" || h.Get("Authorization") != "" || h.Get("X-Organization-ID") != "" {
		t.Fatalf("headers: %v", h)
	}
}

func TestAnExpiredTokenIsRefreshedOnceForConcurrentCalls(t *testing.T) {
	setup(t)
	var refreshes atomic.Int32
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshes.Add(1)
		w.Write([]byte(`{"access_token": "fresh", "refresh_token": "rt2", "expires_in": 300}`))
	}))
	defer issuer.Close()
	u, srv := newUpstream(t, nil)
	saveCreds(t, srv.URL, issuer.URL, 0)
	reqs := []string{initReq}
	for i := 2; i < 8; i++ {
		reqs = append(reqs, `{"jsonrpc": "2.0", "id": `+string(rune('0'+i))+`, "method": "tools/call", "params": {"name": "x"}}`)
	}
	session(t, reqs...)
	if n := refreshes.Load(); n != 1 {
		t.Fatalf("want one refresh, got %d", n)
	}
	for _, c := range u.callsTo("tools/call") {
		if c.Header.Get("Authorization") != "Bearer fresh" {
			t.Fatalf("stale token sent: %v", c.Header)
		}
	}
}

func TestUnreachableKemoryShowsAnExplanatoryTool(t *testing.T) {
	setup(t)
	saveCreds(t, "http://127.0.0.1:1", "https://unused.invalid", time.Hour)
	resp := session(t,
		`{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}`,
		`{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "x"}}`)
	tools := result(t, resp["2"])["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "kemory_unreachable" {
		t.Fatalf("tools: %v", tools)
	}
	text := result(t, resp["3"])["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Cannot connect to kemory") {
		t.Fatalf("call: %s", text)
	}
}

func TestATimeoutWarnsAgainstABlindRetry(t *testing.T) {
	setup(t)
	_, srv := newUpstream(t, func(method string, p json.RawMessage, h http.Header) (int, string) {
		if method == "tools/call" {
			time.Sleep(300 * time.Millisecond)
		}
		return defaultReply(method, p, h)
	})
	saveCreds(t, srv.URL, "https://unused.invalid", time.Hour)
	t.Setenv("KEMORY_HTTP_TIMEOUT", "0.05")
	resp := session(t, `{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "x"}}`)
	text := result(t, resp["3"])["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "MAY have completed") {
		t.Fatalf("got %s", text)
	}
}

func TestEventStreamResponsesAreRead(t *testing.T) {
	setup(t)
	_, srv := newUpstream(t, func(method string, p json.RawMessage, h http.Header) (int, string) {
		if method == "tools/call" {
			return 200, "event: message\ndata: {\"jsonrpc\": \"2.0\", \"id\": 1, \"result\": {\"content\": [{\"type\": \"text\", \"text\": \"sse\"}]}}\n\n"
		}
		return defaultReply(method, p, h)
	})
	saveCreds(t, srv.URL, "https://unused.invalid", time.Hour)
	resp := session(t, `{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "x"}}`)
	if text := result(t, resp["3"])["content"].([]any)[0].(map[string]any)["text"]; text != "sse" {
		t.Fatalf("got %v", text)
	}
}

// hostRouter serves requests for named hosts in-process, so the real
// allowlisted cell hosts can be used.
type hostRouter map[string]http.Handler

func (h hostRouter) RoundTrip(r *http.Request) (*http.Response, error) {
	handler, ok := h[r.URL.Host]
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	return rec.Result(), nil
}

func TestAWrongCellRefusalIsRecoveredOnceAndSaved(t *testing.T) {
	setup(t)
	zurich := &upstream{reply: func(method string, p json.RawMessage, h http.Header) (int, string) {
		if method == "tools/call" {
			return 403, `{"detail": {"code": "ORG_WRONG_RESIDENCY_CELL", "this_cell": "zurich",
				"target_hosts": {"api": "https://api.kemory.in.sekondbrain.ai"}}}`
		}
		return defaultReply(method, p, h)
	}}
	mumbai := &upstream{reply: defaultReply}
	router := hostRouter{"api.kemory.s9n.ai": zurich, "api.kemory.in.sekondbrain.ai": mumbai}
	oldBridge, oldResidency := HTTPClient, residency.HTTPClient
	HTTPClient = &http.Client{Transport: router}
	residency.HTTPClient = &http.Client{Transport: router}
	t.Cleanup(func() { HTTPClient, residency.HTTPClient = oldBridge, oldResidency })

	saveCreds(t, "https://api.kemory.s9n.ai", "https://unused.invalid", time.Hour)
	resp := session(t, `{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "x"}}`)

	content := result(t, resp["3"])["content"].([]any)
	if !strings.Contains(content[0].(map[string]any)["text"].(string), "re-targeted") || content[1].(map[string]any)["text"] != "found 2" {
		t.Fatalf("content: %v", content)
	}
	if len(mumbai.callsTo("tools/call")) != 1 || len(zurich.callsTo("tools/call")) != 1 {
		t.Fatal("want exactly one refused call and one retry")
	}
	if c, _ := config.LoadCredentials("prod"); c.KemoryURL != "https://api.kemory.in.sekondbrain.ai" {
		t.Fatalf("new cell not saved: %s", c.KemoryURL)
	}
}
