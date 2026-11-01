package server

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Access log: one structured line per request — method, path, status,
// duration, organisation. Never query strings (OAuth callbacks carry
// authorization codes), never headers, never bodies; /connect/<id> paths
// are single-use capabilities, so the id is redacted.

type reqInfo struct{ org string }
type reqInfoKey struct{}
type internalKey struct{}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func logPath(p string) string {
	if strings.HasPrefix(p, "/connect/") {
		return "/connect/…"
	}
	return p
}

func (s *Server) accessLog(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// MCP tools run through the REST handlers; those inner calls are logged as the tool call instead.
		if r.Context().Value(internalKey{}) != nil || r.URL.Path == "/healthz" {
			h.ServeHTTP(w, r)
			return
		}
		info := &reqInfo{}
		r = r.WithContext(context.WithValue(r.Context(), reqInfoKey{}, info))
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		h.ServeHTTP(sw, r)
		s.Log.Info("request", "method", r.Method, "path", logPath(r.URL.Path), "status", sw.status,
			"ms", time.Since(start).Milliseconds(), "org", info.org)
	})
}

func noteOrg(ctx context.Context, orgID string) {
	if info, ok := ctx.Value(reqInfoKey{}).(*reqInfo); ok {
		info.org = orgID
	}
}
