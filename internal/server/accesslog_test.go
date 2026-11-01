package server

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestAccessLogRecordsRequestsWithoutSecrets(t *testing.T) {
	e := newEnv(t)
	var buf bytes.Buffer
	e.srv.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	e.connectLinkedIn() // hits /connect/<id> and /oauth/linkedin/callback?code=good-code&state=…
	e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "logged"}, map[string]string{"Idempotency-Key": "log-1"})
	e.tool("list_accounts", nil)
	out := buf.String()

	for _, secret := range []string{e.key, "good-code", "state=", "cs_", "li-token", "Bearer"} {
		if strings.Contains(out, secret) {
			t.Fatalf("access log leaked %q:\n%s", secret, out)
		}
	}
	for _, want := range []string{`"path":"/v1/posts"`, `"status":201`, `"path":"/connect/…"`, `"path":"/oauth/linkedin/callback"`, `"msg":"mcp tool"`, `"tool":"list_accounts"`, `"org":"org_`} {
		if !strings.Contains(out, want) {
			t.Fatalf("access log is missing %s:\n%s", want, out)
		}
	}
	if strings.Count(out, `"path":"/v1/accounts"`) != 0 {
		t.Fatal("an MCP tool's inner REST call must not be logged twice")
	}
}
