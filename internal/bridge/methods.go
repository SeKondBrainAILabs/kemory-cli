package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/residency"
)

func emptySchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// errorResult is a tool result the model can read: the bridge reports every
// failure this way rather than as a protocol error the host cannot show.
func errorResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": true,
	}
}

// stripMeta removes the host's _meta from params before they go upstream; it
// belongs to the host's session, not to Kemory's.
func stripMeta(params json.RawMessage) json.RawMessage {
	var p map[string]json.RawMessage
	if len(params) == 0 || json.Unmarshal(params, &p) != nil {
		return params
	}
	delete(p, "_meta")
	out, err := json.Marshal(p)
	if err != nil {
		return params
	}
	return out
}

// listTools relays tools/list. It never fails: an unreachable or misrouted
// Kemory shows up as a single tool whose description says what to do, because
// a protocol error here shows the user nothing actionable.
func (b *Bridge) listTools(params json.RawMessage) any {
	b.handshake("")
	b.ensureHomeCell()
	for attempt := 0; attempt < 2; attempt++ {
		result, err := b.call("tools/list", stripMeta(params), readTimeout())
		if err == nil {
			return result
		}
		if refusal := refusalOf(err); refusal != nil {
			if attempt == 0 && b.recoverResidency(refusal) != "" {
				continue
			}
			Logf("tools/list refused for residency: %v", err)
			return map[string]any{"tools": []map[string]any{{
				"name":        "kemory_wrong_region",
				"description": wrongRegionToolText(refusal),
				"inputSchema": emptySchema(),
			}}}
		}
		Logf("tools/list failed: %v", err)
		break
	}
	return map[string]any{"tools": []map[string]any{{
		"name": "kemory_unreachable",
		"description": fmt.Sprintf("Kemory at %s is not reachable. "+
			"Run `kemory login` (if you haven't yet) or check the URL.", b.resolveURL()),
		"inputSchema": emptySchema(),
	}}}
}

func wrongRegionToolText(r *residency.Refusal) string {
	text := "This organisation's memories live in a different residency region"
	if r.TargetAPI != "" {
		text += " (API endpoint " + r.TargetAPI + ")"
	}
	text += ". "
	if os.Getenv("KEMORY_URL") != "" {
		return text + "KEMORY_URL is set and pins this bridge to the wrong cell — unset it or point it at the org's region."
	}
	return text + "Automatic re-targeting failed; run `kemory doctor` or upgrade the CLI."
}

// callTool relays tools/call. It never fails at the protocol level: every
// failure becomes an error result the model can read and act on.
func (b *Bridge) callTool(params json.RawMessage) any {
	h := b.headers()
	if h.Get("Authorization") == "" && h.Get("X-API-Key") == "" {
		return errorResult("Kemory has no credentials. Run `kemory login` to authenticate, " +
			"or set KEMORY_API_KEY for API-key auth.")
	}
	b.handshake("")
	b.ensureHomeCell()
	forwarded := normalizeArguments(stripMeta(params))
	notice := ""
	for attempt := 0; attempt < 2; attempt++ {
		result, err := b.call("tools/call", forwarded, readTimeout())
		if err == nil {
			if notice != "" {
				return prependText(result, notice)
			}
			return result
		}
		var rpc *rpcError
		if errors.As(err, &rpc) {
			return errorResult("Vault error: " + rpc.Error())
		}
		// A wrong-cell refusal is the only failure retried, once: the server
		// refuses before any handler runs, so the retry cannot duplicate a
		// write. A timeout must never be retried.
		if refusal := refusalOf(err); refusal != nil {
			if attempt == 0 {
				if url := b.recoverResidency(refusal); url != "" {
					notice = "(kemory: re-targeted to your org's data region at " + url + ")\n"
					continue
				}
			}
			msg := residency.WrongRegionMessage(refusal, b.resolveURL())
			if os.Getenv("KEMORY_URL") != "" {
				msg += "\nKEMORY_URL is set and pins this bridge to that URL — unset it or point it at the org's region."
			}
			return errorResult(msg)
		}
		var up *upstreamError
		if errors.As(err, &up) {
			return errorResult(up.Error())
		}
		if isTimeout(err) {
			return errorResult(fmt.Sprintf("Bridge timeout after %s did not respond in time. The server MAY have "+
				"completed the operation — for writes, verify with kemory_list_namespaces or kemory_recall_memory "+
				"BEFORE retrying so you don't create a duplicate. Raise KEMORY_HTTP_TIMEOUT (seconds) if this is recurring.",
				b.resolveURL()))
		}
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return errorResult(fmt.Sprintf("Cannot connect to kemory at %s.\nHints:\n"+
				"  • Is the URL correct? Try `kemory doctor`.\n"+
				"  • Set KEMORY_URL=<url> if you need to override.\n"+
				"  • For local dev, run `docker compose up -d kemory-api`.", b.resolveURL()))
		}
		return errorResult("Bridge error: " + err.Error())
	}
	return errorResult("Bridge error: residency retry did not settle")
}

// normalizeArguments sends `arguments: {}` when the host omitted it; the
// protocol allows omitting it for a tool that takes no input.
func normalizeArguments(params json.RawMessage) json.RawMessage {
	var p map[string]json.RawMessage
	if json.Unmarshal(params, &p) != nil {
		return params
	}
	if raw, ok := p["arguments"]; !ok || string(raw) == "null" {
		p["arguments"] = json.RawMessage("{}")
	}
	out, err := json.Marshal(p)
	if err != nil {
		return params
	}
	return out
}

// prependText adds a text item at the start of a tool result's content.
func prependText(result json.RawMessage, text string) json.RawMessage {
	var r map[string]json.RawMessage
	if json.Unmarshal(result, &r) != nil {
		return result
	}
	var content []json.RawMessage
	_ = json.Unmarshal(r["content"], &content)
	item, _ := json.Marshal(map[string]any{"type": "text", "text": text})
	r["content"], _ = json.Marshal(append([]json.RawMessage{item}, content...))
	out, err := json.Marshal(r)
	if err != nil {
		return result
	}
	return out
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// forward relays one method and returns its result, or the error to give the
// host: the server's own JSON-RPC error, or -32603 when Kemory is unreachable.
func (b *Bridge) forward(method string, params json.RawMessage) (json.RawMessage, *rpcError) {
	b.handshake("")
	b.ensureHomeCell()
	result, err := b.call(method, stripMeta(params), readTimeout())
	if err == nil {
		return result, nil
	}
	var rpc *rpcError
	if errors.As(err, &rpc) {
		return nil, rpc
	}
	return nil, &rpcError{Code: codeInternal, Message: fmt.Sprintf("Kemory unreachable: %v", err)}
}

func (b *Bridge) relay(msg message) {
	result, rpc := b.forward(msg.Method, msg.Params)
	if rpc != nil {
		b.fail(msg.ID, rpc)
		return
	}
	b.write(message{JSONRPC: "2.0", ID: msg.ID, Result: result})
}

// relayList relays a list method; a list that cannot be fetched is empty
// rather than an error that would break the host's UI.
func (b *Bridge) relayList(msg message, key string) {
	result, rpc := b.forward(msg.Method, msg.Params)
	if rpc != nil {
		Logf("%s failed: %v", msg.Method, rpc)
		b.reply(msg.ID, map[string]any{key: []any{}})
		return
	}
	b.write(message{JSONRPC: "2.0", ID: msg.ID, Result: result})
}
