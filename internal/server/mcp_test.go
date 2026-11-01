package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func (e *env) rpc(key string, id any, method string, params any) (int, map[string]any) {
	e.t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		msg["id"] = id
	}
	if params != nil {
		msg["params"] = params
	}
	raw, _ := json.Marshal(msg)
	req, _ := http.NewRequest("POST", e.api.URL+"/mcp", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return resp.StatusCode, out
}

// call runs tools/call and returns the structured REST status and body.
func (e *env) tool(name string, args map[string]any) (int, map[string]any, map[string]any) {
	e.t.Helper()
	_, out := e.rpc(e.key, 1, "tools/call", map[string]any{"name": name, "arguments": args})
	res, ok := out["result"].(map[string]any)
	if !ok {
		e.t.Fatalf("tools/call %s: %v", name, out)
	}
	sc := res["structuredContent"].(map[string]any)
	body, _ := sc["body"].(map[string]any)
	if (int(sc["status"].(float64)) >= 400) != res["isError"].(bool) {
		e.t.Fatalf("isError must match the status: %v", res)
	}
	return int(sc["status"].(float64)), body, sc
}

func TestMCPHandshakeAndTools(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.rpc("", 1, "initialize", nil); code != 401 {
		t.Fatalf("MCP needs the API key, got %d", code)
	}
	_, init := e.rpc(e.key, 1, "initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "banyan", "version": "1"}})
	r := init["result"].(map[string]any)
	if r["protocolVersion"] != "2025-03-26" || r["serverInfo"].(map[string]any)["name"] != "socialflow-lite" {
		t.Fatalf("initialize: %v", init)
	}
	if _, latest := e.rpc(e.key, 2, "initialize", map[string]any{"protocolVersion": "1999-01-01"}); latest["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatal("an unknown client version gets the newest supported one")
	}
	if code, _ := e.rpc(e.key, nil, "notifications/initialized", nil); code != 202 {
		t.Fatalf("notifications are acknowledged with 202, got %d", code)
	}
	_, list := e.rpc(e.key, 3, "tools/list", nil)
	tools := list["result"].(map[string]any)["tools"].([]any)
	byName := map[string]map[string]any{}
	for _, x := range tools {
		m := x.(map[string]any)
		byName[m["name"].(string)] = m
	}
	for _, want := range []string{"whoami", "list_accounts", "get_account_capabilities", "start_account_connection", "publish_post", "schedule_post", "get_post_status", "disconnect_account"} {
		if byName[want] == nil {
			t.Fatalf("missing tool %s", want)
		}
	}
	if byName["disconnect_account"]["annotations"].(map[string]any)["destructiveHint"] != true {
		t.Fatal("disconnect must be marked destructive")
	}
	if byName["list_accounts"]["annotations"].(map[string]any)["readOnlyHint"] != true {
		t.Fatal("list_accounts must be read-only")
	}
	req := byName["publish_post"]["inputSchema"].(map[string]any)["required"].([]any)
	if len(req) != 3 {
		t.Fatalf("publish_post must require channel, text and idempotency_key: %v", req)
	}
	if _, bad := e.rpc(e.key, 4, "tools/call", map[string]any{"name": "nope"}); bad["error"] == nil {
		t.Fatal("an unknown tool is a protocol error")
	}
	if _, bad := e.rpc(e.key, 5, "resources/list", nil); bad["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatal("unknown methods answer -32601")
	}
}

func TestMCPPublishIsTheRESTPublish(t *testing.T) {
	e := newEnv(t)
	e.connectLinkedIn()
	if status, body, _ := e.tool("whoami", nil); status != 200 || body["organization_id"] == nil {
		t.Fatalf("whoami: %d %v", status, body)
	}
	args := map[string]any{"channel": "linkedin", "text": "Shipped over MCP", "idempotency_key": "mcp-1"}
	status, post, _ := e.tool("publish_post", args)
	if status != 201 || post["status"] != "published" {
		t.Fatalf("publish: %d %v", status, post)
	}
	if status, again, _ := e.tool("publish_post", args); status != 200 || again["id"] != post["id"] || len(e.fake.linkedInPosts) != 1 {
		t.Fatalf("MCP replay must not post twice: %d %v posts=%d", status, again, len(e.fake.linkedInPosts))
	}
	// The same key through REST finds the same post: one idempotency store, whatever the transport.
	rest := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "Shipped over MCP"}, map[string]string{"Idempotency-Key": "mcp-1"})
	if rest.body["id"] != post["id"] {
		t.Fatalf("REST and MCP must share idempotency: %v", rest.raw)
	}
	if status, got, _ := e.tool("get_post_status", map[string]any{"post_id": post["id"]}); status != 200 || got["url"] == nil {
		t.Fatalf("status: %d %v", status, got)
	}
	if status, _, _ := e.tool("get_post_status", map[string]any{}); status != 400 {
		t.Fatal("a missing id is a tool error, not a crash")
	}
	if status, body, _ := e.tool("publish_post", map[string]any{"channel": "shortvideo", "text": "x", "idempotency_key": "v"}); status != 422 || body["error"].(map[string]any)["code"] != "unsupported_format" {
		t.Fatalf("contract errors pass through: %d %v", status, body)
	}
}

func TestMCPRateLimitCarriesRetryAfter(t *testing.T) {
	e := newEnv(t)
	e.connectLinkedIn()
	e.fake.liStatus, e.fake.liRetryAfter = 429, "9"
	status, body, sc := e.tool("publish_post", map[string]any{"channel": "linkedin", "text": "hi", "idempotency_key": "rl"})
	if status != 429 || body["error"].(map[string]any)["code"] != "rate_limited" || sc["retry_after"].(float64) < 9 {
		t.Fatalf("rate limit: %d %v %v", status, body, sc)
	}
}
