package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
)

// MCP: the same operations as the REST contract (C1–C10), exposed as Model
// Context Protocol tools over Streamable HTTP (stateless, JSON responses) at
// POST /mcp. Authentication is the same Bearer API key.
//
// Every tool is executed by the REST handler it mirrors, so validation,
// idempotency, error codes and limits are identical whichever way a client
// calls. Each result carries the REST status and body in structuredContent,
// so a client can apply exactly the contract's retry and error rules.

const mcpServerName = "socialflow-lite"

// Newest first; the server answers with the client's version when it supports it.
var mcpVersions = []string{"2025-06-18", "2025-03-26"}

type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations"`
	method      string
	path        func(args map[string]any) (string, bool)
	body        func(args map[string]any) map[string]any
	header      func(args map[string]any) map[string]string
}

func str(args map[string]any, k string) string {
	s, _ := args[k].(string)
	return strings.TrimSpace(s)
}

func obj(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

var (
	pString      = map[string]any{"type": "string"}
	pChannel     = map[string]any{"type": "string", "enum": []string{"linkedin", "x"}, "description": "Where to publish."}
	pText        = map[string]any{"type": "string", "description": "The approved post text. An X thread is blank-line-separated posts, the first starting \"1/\"."}
	pAccount     = map[string]any{"type": "string", "description": "Account id from list_accounts. Omit to use the single active account for the channel."}
	pIdempotency = map[string]any{"type": "string", "description": "Required. Reusing the key returns the same post (and resumes a partly posted thread) instead of posting twice."}
)

func pathParam(prefix, key, suffix string) func(map[string]any) (string, bool) {
	return func(a map[string]any) (string, bool) {
		v := str(a, key)
		if v == "" {
			return "", false
		}
		return prefix + url.PathEscape(v) + suffix, true
	}
}

func fixed(p string) func(map[string]any) (string, bool) {
	return func(map[string]any) (string, bool) { return p, true }
}

func postBody(a map[string]any) map[string]any {
	b := map[string]any{"channel": str(a, "channel"), "text": a["text"]}
	for _, k := range []string{"account_id", "title", "scheduled_at"} {
		if v := str(a, k); v != "" {
			b[k] = v
		}
	}
	return b
}

func idempotency(a map[string]any) map[string]string {
	return map[string]string{"Idempotency-Key": str(a, "idempotency_key")}
}

var mcpTools = []mcpTool{
	{
		Name: "whoami", Title: "Who am I", Description: "The organisation this API key belongs to (C1).",
		InputSchema: obj(map[string]any{}), Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false},
		method: http.MethodGet, path: fixed("/v1/me"),
	},
	{
		Name: "list_accounts", Title: "List connected accounts",
		Description: "Connected LinkedIn and X accounts with status (active | error = reconnect | revoked), capabilities and token expiry (C3).",
		InputSchema: obj(map[string]any{}), Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false},
		method: http.MethodGet, path: fixed("/v1/accounts"),
	},
	{
		Name: "get_account_capabilities", Title: "Account capabilities", Description: "What one account can do: publish, schedule, analytics, formats, max length (C4).",
		InputSchema: obj(map[string]any{"account_id": pString}, "account_id"), Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false},
		method: http.MethodGet, path: pathParam("/v1/accounts/", "account_id", "/capabilities"),
	},
	{
		Name: "start_account_connection", Title: "Start connecting an account",
		Description: "Returns a single-use connect_url (30 minutes) for a person to open and sign in to LinkedIn or X. Only a human can complete it (C2).",
		InputSchema: obj(map[string]any{"platform": pChannel}, "platform"), Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "openWorldHint": false},
		method: http.MethodPost, path: fixed("/v1/accounts/connect"),
		body: func(a map[string]any) map[string]any { return map[string]any{"platform": str(a, "platform")} },
	},
	{
		Name: "publish_post", Title: "Publish an approved post now",
		Description: "Publishes text that a human has approved to LinkedIn or X, under that person's account (C5). Do not call it for unapproved copy. " +
			"status \"published\" means the platform confirmed it; a 429 or 502 status means retry later with the same idempotency_key.",
		InputSchema: obj(map[string]any{"channel": pChannel, "text": pText, "account_id": pAccount, "idempotency_key": pIdempotency}, "channel", "text", "idempotency_key"),
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true},
		method:      http.MethodPost, path: fixed("/v1/posts"), body: postBody, header: idempotency,
	},
	{
		Name: "schedule_post", Title: "Schedule an approved post",
		Description: "Schedules approved text for a future time (RFC 3339 with offset); SocialFlow Lite publishes it then (C6). Same approval rule as publish_post.",
		InputSchema: obj(map[string]any{"channel": pChannel, "text": pText, "scheduled_at": map[string]any{"type": "string", "format": "date-time"}, "account_id": pAccount, "idempotency_key": pIdempotency}, "channel", "text", "scheduled_at", "idempotency_key"),
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true},
		method:      http.MethodPost, path: fixed("/v1/posts"), body: postBody, header: idempotency,
	},
	{
		Name: "get_post_status", Title: "Post status", Description: "queued | scheduled | published | failed, with the live URL and the error if any (C7).",
		InputSchema: obj(map[string]any{"post_id": pString}, "post_id"), Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false},
		method: http.MethodGet, path: pathParam("/v1/posts/", "post_id", ""),
	},
	{
		Name: "cancel_post", Title: "Cancel a scheduled post",
		Description: "Stops a scheduled (or failed) post before it goes out (C8). A post already published cannot be cancelled here.",
		InputSchema: obj(map[string]any{"post_id": pString}, "post_id"), Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false},
		method: http.MethodDelete, path: pathParam("/v1/posts/", "post_id", ""),
	},
	{
		Name: "disconnect_account", Title: "Disconnect an account",
		Description: "Revokes an account: its token is destroyed here and revocation is requested from the platform (C10). Publishing to it stops until a person reconnects.",
		InputSchema: obj(map[string]any{"account_id": pString}, "account_id"), Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": true},
		method: http.MethodDelete, path: pathParam("/v1/accounts/", "account_id", ""),
	},
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func rpcReply(w http.ResponseWriter, id json.RawMessage, result any, rerr *rpcError) {
	resp := map[string]any{"jsonrpc": "2.0", "id": id}
	if rerr != nil {
		resp["error"] = rerr
	} else {
		resp["result"] = result
	}
	writeJSON(w, http.StatusOK, resp)
}

// mcp serves POST /mcp (JSON-RPC 2.0). GET is not offered: this server sends no
// unsolicited messages, which the Streamable HTTP transport allows.
func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(&req); err != nil || req.JSONRPC != "2.0" || req.Method == "" {
		rpcReply(w, nil, nil, &rpcError{Code: -32700, Message: "Parse error: expected one JSON-RPC 2.0 request"})
		return
	}
	// Notifications (no id) are acknowledged without a body.
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := mcpVersions[0]
		for _, v := range mcpVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		rpcReply(w, req.ID, map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": mcpServerName, "title": "SocialFlow Lite", "version": "0.2.0"},
			"instructions": "Publish only text a human has approved. Always pass an idempotency_key and reuse it on retry. " +
				"Accounts are connected by a person through start_account_connection; an agent cannot sign in for them.",
		}, nil)
	case "ping":
		rpcReply(w, req.ID, map[string]any{}, nil)
	case "tools/list":
		rpcReply(w, req.ID, map[string]any{"tools": mcpTools}, nil)
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			rpcReply(w, req.ID, nil, &rpcError{Code: -32602, Message: "Invalid params"})
			return
		}
		var tool *mcpTool
		for i := range mcpTools {
			if mcpTools[i].Name == p.Name {
				tool = &mcpTools[i]
			}
		}
		if tool == nil {
			rpcReply(w, req.ID, nil, &rpcError{Code: -32602, Message: "Unknown tool: " + p.Name})
			return
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		rpcReply(w, req.ID, s.runTool(r, tool, p.Arguments), nil)
	default:
		rpcReply(w, req.ID, nil, &rpcError{Code: -32601, Message: "Method not found: " + req.Method})
	}
}

// runTool executes the tool through the REST handler it mirrors.
func (s *Server) runTool(r *http.Request, t *mcpTool, args map[string]any) map[string]any {
	path, ok := t.path(args)
	if !ok {
		return toolResult(http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "validation", "message": "A required id argument is missing."}}, "")
	}
	var body io.Reader
	if t.body != nil {
		raw, _ := json.Marshal(t.body(args))
		body = bytes.NewReader(raw)
	}
	inner := httptest.NewRequest(t.method, path, body).WithContext(context.WithValue(r.Context(), internalKey{}, true))
	inner.Header.Set("Authorization", r.Header.Get("Authorization"))
	inner.Header.Set("Content-Type", "application/json")
	if t.header != nil {
		for k, v := range t.header(args) {
			if v != "" {
				inner.Header.Set(k, v)
			}
		}
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, inner)
	if info, ok := r.Context().Value(reqInfoKey{}).(*reqInfo); ok {
		s.Log.Info("mcp tool", "tool", t.Name, "status", rec.Code, "org", info.org)
	}
	var parsed any
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		parsed = map[string]any{}
	}
	return toolResult(rec.Code, parsed, rec.Header().Get("Retry-After"))
}

func toolResult(status int, body any, retryAfter string) map[string]any {
	structured := map[string]any{"status": status, "body": body}
	if retryAfter != "" {
		if n, err := strconv.Atoi(retryAfter); err == nil {
			structured["retry_after"] = n
		}
	}
	text, _ := json.Marshal(structured)
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(text)}},
		"structuredContent": structured,
		"isError":           status >= 400,
	}
}
