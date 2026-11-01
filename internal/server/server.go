// Package server exposes the Banyan ↔ SocialFlow contract
// (banyan-socialflow-contract/0.1, operations C1–C10) over HTTP.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kael-tombo/socialflow-lite/internal/providers"
	"github.com/kael-tombo/socialflow-lite/internal/store"
)

const (
	connectSessionTTL = 30 * time.Minute
	maxBodyBytes      = 256 << 10
	// A scheduled_at closer than this is treated as "publish now".
	scheduleThreshold = 30 * time.Second
)

type Server struct {
	Store     *store.Store
	Sealer    *store.Sealer
	LinkedIn  *providers.LinkedIn
	X         *providers.X
	PublicURL string
	Log       *slog.Logger
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })

	// Contract operations (Bearer API key).
	mux.Handle("GET /v1/me", s.auth(s.me))                                   // C1
	mux.Handle("POST /v1/accounts/connect", s.auth(s.connect))               // C2
	mux.Handle("GET /v1/accounts", s.auth(s.accounts))                       // C3
	mux.Handle("GET /v1/accounts/{id}/capabilities", s.auth(s.capabilities)) // C4
	mux.Handle("POST /v1/posts", s.auth(s.createPost))                       // C5, C6
	mux.Handle("GET /v1/posts/{id}", s.auth(s.getPost))                      // C7
	mux.Handle("GET /v1/posts/{id}/analytics", s.auth(s.analytics))          // C9
	mux.Handle("DELETE /v1/accounts/{id}", s.auth(s.revoke))                 // C10

	// The same operations as MCP tools (Model Context Protocol, Streamable HTTP, stateless).
	mux.Handle("POST /mcp", s.auth(s.mcp))

	// Browser-facing connect flow (the connect_url is a single-use capability).
	mux.HandleFunc("GET /connect/{id}", s.connectRedirect)
	mux.HandleFunc("GET /oauth/linkedin/callback", s.linkedInCallback)
	mux.HandleFunc("GET /oauth/x/callback", s.xCallback)

	return s.accessLog(securityHeaders(mux))
}

// ───────── plumbing ─────────

type ctxKey struct{}

func orgFrom(r *http.Request) store.Org { return r.Context().Value(ctxKey{}).(store.Org) }

func (s *Server) auth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(key) == "" {
			writeErr(w, 401, providers.CodeAuthFailed, "Missing bearer API key.")
			return
		}
		org, err := s.Store.OrgByKey(r.Context(), strings.TrimSpace(key))
		if err != nil {
			writeErr(w, 401, providers.CodeAuthFailed, "Invalid or revoked API key.")
			return
		}
		noteOrg(r.Context(), org.ID)
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, org)))
	})
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (s *Server) internal(w http.ResponseWriter, err error) {
	s.Log.Error("internal error", "err", err)
	writeErr(w, 500, providers.CodeProviderUnavailable, "Internal error.")
}

func (s *Server) provider(platform string) (configured bool, caps providers.Capabilities, known bool) {
	switch platform {
	case "linkedin":
		return s.LinkedIn.Configured(), s.LinkedIn.Capabilities(), true
	case "x":
		return s.X.Configured(), s.X.Capabilities(), true
	}
	return false, providers.Capabilities{}, false
}

// ───────── C1–C4, C10 ─────────

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	org := orgFrom(r)
	writeJSON(w, 200, map[string]string{"organization_id": org.ID, "name": org.Name})
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Platform string `json:"platform"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeErr(w, 400, providers.CodeValidation, "Body must be JSON with a platform.")
		return
	}
	configured, _, known := s.provider(body.Platform)
	if !known {
		writeErr(w, 422, providers.CodeUnsupportedFormat, "SocialFlow Lite supports linkedin and x.")
		return
	}
	if !configured {
		writeErr(w, 503, providers.CodeProviderUnavailable, body.Platform+" is not configured on this SocialFlow Lite instance.")
		return
	}
	cs, err := s.Store.CreateConnectSession(r.Context(), orgFrom(r).ID, body.Platform)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{
		"connect_url": s.PublicURL + "/connect/" + cs.ID,
		"expires_at":  cs.CreatedAt.Add(connectSessionTTL).Format(time.RFC3339),
	})
}

type accountJSON struct {
	ID           string                 `json:"id"`
	Platform     string                 `json:"platform"`
	Handle       string                 `json:"handle"`
	Status       string                 `json:"status"`
	Capabilities providers.Capabilities `json:"capabilities"`
	// When the platform token stops working (LinkedIn: ~60 days); null when it does not expire (X).
	TokenExpiresAt *time.Time `json:"token_expires_at"`
}

func (s *Server) accountView(a store.Account) accountJSON {
	_, caps, _ := s.provider(a.Platform)
	status := a.Status
	// An expired token is reported as needing reconnection now, not when the next post fails.
	if status == "active" && a.TokenExpiresAt != nil && time.Now().After(*a.TokenExpiresAt) {
		status = "error"
	}
	if status != "active" {
		caps.Publish, caps.Schedule = false, false
	}
	return accountJSON{ID: a.ID, Platform: a.Platform, Handle: a.Handle, Status: status, Capabilities: caps, TokenExpiresAt: a.TokenExpiresAt}
}

func (s *Server) accounts(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Accounts(r.Context(), orgFrom(r).ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	out := make([]accountJSON, 0, len(list))
	for _, a := range list {
		out = append(out, s.accountView(a))
	}
	writeJSON(w, 200, map[string]any{"accounts": out})
}

func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.Account(r.Context(), orgFrom(r).ID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, providers.CodeNotFound, "No such account.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, 200, s.accountView(a).Capabilities)
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.Account(r.Context(), orgFrom(r).ID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, providers.CodeNotFound, "No such account.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	providerRevoked := false
	if a.SealedToken != "" {
		if tok, err := s.Sealer.Open(a.SealedToken); err == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			switch a.Platform {
			case "linkedin":
				providerRevoked = s.LinkedIn.Revoke(ctx, tok) == nil
			case "x":
				if t, sec, ok := strings.Cut(tok, "|"); ok {
					providerRevoked = s.X.Revoke(ctx, t, sec) == nil
				}
			}
		}
	}
	if err := s.Store.SetAccountStatus(r.Context(), a.ID, "revoked"); err != nil {
		s.internal(w, err)
		return
	}
	// The local token is always destroyed; provider_revoked reports whether
	// the provider also confirmed it (otherwise remove the app in its settings).
	writeJSON(w, 200, map[string]any{"id": a.ID, "status": "revoked", "provider_revoked": providerRevoked})
}

// ───────── C5–C7, C9 ─────────

type postJSON struct {
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	URL         *string           `json:"url"`
	Error       map[string]string `json:"error"`
	AccountID   string            `json:"account_id"`
	Channel     string            `json:"channel"`
	ExternalIDs []string          `json:"external_ids"`
	ScheduledAt *time.Time        `json:"scheduled_at"`
	PublishedAt *time.Time        `json:"published_at"`
	Attempts    int               `json:"attempts"`
}

func postView(p store.Post) postJSON {
	status := p.Status
	if status == "pending" {
		status = "queued" // internal pre-claim state; the contract has no pending
	}
	v := postJSON{ID: p.ID, Status: status, AccountID: p.AccountID, Channel: p.Platform, ExternalIDs: p.ExternalIDs,
		ScheduledAt: p.ScheduledAt, PublishedAt: p.PublishedAt, Attempts: p.Attempts}
	if v.ExternalIDs == nil {
		v.ExternalIDs = []string{}
	}
	if p.URL != "" {
		u := p.URL
		v.URL = &u
	}
	if p.Status == "failed" && p.ErrorCode != "" {
		v.Error = map[string]string{"code": p.ErrorCode, "message": p.ErrorMessage}
	}
	return v
}

func (s *Server) createPost(w http.ResponseWriter, r *http.Request) {
	org := orgFrom(r)
	var body struct {
		AccountID   string `json:"account_id"`
		Channel     string `json:"channel"`
		Text        string `json:"text"`
		Title       string `json:"title"`
		ScheduledAt string `json:"scheduled_at"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeErr(w, 400, providers.CodeValidation, "Body must be JSON.")
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" || body.Channel == "" {
		writeErr(w, 400, providers.CodeValidation, "channel and text are required.")
		return
	}
	if _, _, known := s.provider(body.Channel); !known {
		writeErr(w, 422, providers.CodeUnsupportedFormat, "SocialFlow Lite publishes to linkedin and x only; "+body.Channel+" is not supported.")
		return
	}
	var scheduledAt *time.Time
	if body.ScheduledAt != "" {
		t, err := time.Parse(time.RFC3339, body.ScheduledAt)
		if err != nil {
			writeErr(w, 400, providers.CodeValidation, "scheduled_at must be an RFC 3339 timestamp with offset.")
			return
		}
		if t.After(time.Now().Add(scheduleThreshold)) {
			t = t.UTC()
			scheduledAt = &t
		}
	}
	if body.Channel == "x" {
		if verr := providers.ValidateThread(providers.SplitThread(body.Text)); verr != nil {
			writeErr(w, 422, verr.Code, verr.Message)
			return
		}
	}

	account, status, code, msg := s.resolveAccount(r.Context(), org.ID, body.Channel, body.AccountID)
	if code != "" {
		writeErr(w, status, code, msg)
		return
	}

	sum := sha256.Sum256([]byte(account.ID + "\x00" + body.Channel + "\x00" + body.Text + "\x00" + body.Title + "\x00" + body.ScheduledAt))
	initial := "pending"
	if scheduledAt != nil {
		initial = "scheduled"
	}
	post, existing, err := s.Store.CreatePost(r.Context(), store.Post{
		OrgID: org.ID, AccountID: account.ID, Platform: body.Channel, IdemKey: strings.TrimSpace(r.Header.Get("Idempotency-Key")),
		BodyHash: hex.EncodeToString(sum[:]), Text: body.Text, Title: body.Title, ScheduledAt: scheduledAt, Status: initial,
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	if existing && post.BodyHash != hex.EncodeToString(sum[:]) {
		writeErr(w, 422, providers.CodeValidation, "This Idempotency-Key was already used for a different request.")
		return
	}

	// Publish now: a new immediate post, or a retry of a failed one with the
	// same key (resumes a thread; a failed scheduled post is past its time).
	if post.Status == "pending" || (existing && post.Status == "failed") {
		post, err = s.execute(r.Context(), post, "pending", "failed")
		if err != nil {
			s.internal(w, err)
			return
		}
	}

	httpStatus := 201
	if existing {
		httpStatus = 200
	}
	// Transient provider failures are signalled as errors so the caller retries
	// (with the same key, which resumes); permanent ones are a failed post.
	if post.Status == "failed" {
		switch post.ErrorCode {
		case providers.CodeRateLimited:
			if ra := s.lastRetryAfter(post.ID); ra > 0 {
				w.Header().Set("Retry-After", itoa(int(ra.Seconds())+1))
			}
			writeErr(w, 429, post.ErrorCode, post.ErrorMessage)
			return
		case providers.CodeProviderUnavailable:
			writeErr(w, 502, post.ErrorCode, post.ErrorMessage)
			return
		}
	}
	writeJSON(w, httpStatus, postView(post))
}

func (s *Server) resolveAccount(ctx context.Context, orgID, channel, accountID string) (store.Account, int, string, string) {
	if accountID != "" {
		a, err := s.Store.Account(ctx, orgID, accountID)
		if err != nil {
			return a, 404, providers.CodeNotFound, "No such account."
		}
		if a.Platform != channel {
			return a, 422, providers.CodeValidation, "account_id is a " + a.Platform + " account, not " + channel + "."
		}
		if a.Status == "revoked" {
			return a, 409, providers.CodeRevoked, "This account was disconnected; reconnect it first."
		}
		if a.Status != "active" {
			return a, 409, providers.CodeAuthFailed, "This account needs to be reconnected."
		}
		return a, 0, "", ""
	}
	list, err := s.Store.Accounts(ctx, orgID)
	if err != nil {
		return store.Account{}, 500, providers.CodeProviderUnavailable, "Internal error."
	}
	var active []store.Account
	needsReconnect := false
	for _, a := range list {
		if a.Platform == channel {
			if a.Status == "active" {
				active = append(active, a)
			} else if a.Status == "error" {
				needsReconnect = true
			}
		}
	}
	switch {
	case len(active) == 1:
		return active[0], 0, "", ""
	case len(active) > 1:
		return store.Account{}, 422, providers.CodeValidation, "Several " + channel + " accounts are connected; pass account_id."
	case needsReconnect:
		return store.Account{}, 409, providers.CodeAuthFailed, "The " + channel + " account needs to be reconnected."
	default:
		return store.Account{}, 422, providers.CodeValidation, "No " + channel + " account is connected."
	}
}

func (s *Server) getPost(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.Post(r.Context(), orgFrom(r).ID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, providers.CodeNotFound, "No such post.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, 200, postView(p))
}

// analytics is honestly unsupported: SocialFlow Lite collects no metrics.
// Banyan maps not_found here to "analytics unsupported".
func (s *Server) analytics(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Store.Post(r.Context(), orgFrom(r).ID, r.PathValue("id")); err != nil {
		writeErr(w, 404, providers.CodeNotFound, "No such post.")
		return
	}
	writeErr(w, 404, providers.CodeNotFound, "SocialFlow Lite does not collect post analytics.")
}

// ───────── connect flow pages ─────────

var page = template.Must(template.New("p").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}}</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 16px;color:#1d2a22;background:#f7f5ef}
h1{font-size:1.4rem}p{color:#4a5a50}</style></head><body><h1>{{.Title}}</h1><p>{{.Body}}</p></body></html>`))

func renderPage(w http.ResponseWriter, status int, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	_ = page.Execute(w, map[string]string{"Title": title, "Body": body})
}

func (s *Server) freshSession(cs store.ConnectSession, err error) (store.ConnectSession, bool) {
	if err != nil || cs.Used || time.Since(cs.CreatedAt) > connectSessionTTL {
		return cs, false
	}
	return cs, true
}

func (s *Server) connectRedirect(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.freshSession(s.Store.ConnectSession(r.Context(), r.PathValue("id")))
	if !ok {
		renderPage(w, 410, "This link has expired", "Start the connection again from the app that sent you here.")
		return
	}
	switch cs.Platform {
	case "linkedin":
		if err := s.Store.SetSessionLookup(r.Context(), cs.ID, "li:"+cs.ID, ""); err != nil {
			s.Log.Error("connect", "err", err)
			renderPage(w, 500, "Something went wrong", "Try again in a moment.")
			return
		}
		http.Redirect(w, r, s.LinkedIn.AuthURL(cs.ID), http.StatusFound)
	case "x":
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		tok, secret, authURL, err := s.X.RequestToken(ctx)
		if err != nil {
			s.Log.Warn("x request token failed", "err", err)
			renderPage(w, 502, "X is not responding", "X did not start the sign-in. Try again in a moment.")
			return
		}
		sealed, err := s.Sealer.Seal(secret)
		if err == nil {
			err = s.Store.SetSessionLookup(r.Context(), cs.ID, "x:"+tok, sealed)
		}
		if err != nil {
			s.Log.Error("connect", "err", err)
			renderPage(w, 500, "Something went wrong", "Try again in a moment.")
			return
		}
		http.Redirect(w, r, authURL, http.StatusFound)
	default:
		renderPage(w, 400, "Unsupported platform", "")
	}
}

func (s *Server) linkedInCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("error") != "" {
		renderPage(w, 400, "LinkedIn connection cancelled", "Nothing was connected. You can close this tab.")
		return
	}
	cs, ok := s.freshSession(s.Store.ConnectSessionByLookup(r.Context(), "li:"+q.Get("state")))
	if !ok || q.Get("code") == "" {
		renderPage(w, 410, "This link has expired", "Start the connection again from the app that sent you here.")
		return
	}
	if used, err := s.Store.UseSession(r.Context(), cs.ID); err != nil || !used {
		renderPage(w, 410, "This link was already used", "Start the connection again from the app that sent you here.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	token, expires, err := s.LinkedIn.Exchange(ctx, q.Get("code"))
	var profile providers.Profile
	if err == nil {
		profile, err = s.LinkedIn.Me(ctx, token)
	}
	if err != nil {
		s.Log.Warn("linkedin connect failed", "err", err)
		renderPage(w, 502, "LinkedIn connection failed", "LinkedIn did not complete the sign-in. Start again from the app.")
		return
	}
	s.finishConnect(w, r.Context(), cs, "linkedin", profile, token, expires)
}

func (s *Server) xCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("denied") != "" {
		renderPage(w, 400, "X connection cancelled", "Nothing was connected. You can close this tab.")
		return
	}
	cs, ok := s.freshSession(s.Store.ConnectSessionByLookup(r.Context(), "x:"+q.Get("oauth_token")))
	if !ok || q.Get("oauth_verifier") == "" {
		renderPage(w, 410, "This link has expired", "Start the connection again from the app that sent you here.")
		return
	}
	secret, err := s.Sealer.Open(cs.Secret)
	if err != nil {
		renderPage(w, 410, "This link has expired", "Start the connection again from the app that sent you here.")
		return
	}
	if used, err := s.Store.UseSession(r.Context(), cs.ID); err != nil || !used {
		renderPage(w, 410, "This link was already used", "Start the connection again from the app that sent you here.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	tok, sec, err := s.X.AccessToken(ctx, q.Get("oauth_token"), secret, q.Get("oauth_verifier"))
	var profile providers.Profile
	if err == nil {
		profile, err = s.X.Me(ctx, tok, sec)
	}
	if err != nil {
		s.Log.Warn("x connect failed", "err", err)
		renderPage(w, 502, "X connection failed", "X did not complete the sign-in. Start again from the app.")
		return
	}
	s.finishConnect(w, r.Context(), cs, "x", profile, tok+"|"+sec, time.Time{})
}

func (s *Server) finishConnect(w http.ResponseWriter, ctx context.Context, cs store.ConnectSession, platform string, p providers.Profile, token string, expires time.Time) {
	sealed, err := s.Sealer.Seal(token)
	if err == nil {
		_, err = s.Store.UpsertAccount(ctx, cs.OrgID, platform, p.ID, p.Handle, sealed, expires)
	}
	if err != nil {
		s.Log.Error("saving account", "err", err)
		renderPage(w, 500, "Something went wrong", "The account could not be saved. Start again from the app.")
		return
	}
	s.Log.Info("account connected", "org", cs.OrgID, "platform", platform)
	renderPage(w, 200, "Connected "+p.Handle, "Your "+map[string]string{"linkedin": "LinkedIn", "x": "X"}[platform]+" account is connected. You can close this tab and return to the app.")
}
