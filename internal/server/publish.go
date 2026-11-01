package server

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/kael-tombo/socialflow-lite/internal/providers"
	"github.com/kael-tombo/socialflow-lite/internal/store"
)

var retryAfters sync.Map // post id → time.Duration, from the last rate-limited attempt

func (s *Server) lastRetryAfter(postID string) time.Duration {
	if v, ok := retryAfters.Load(postID); ok {
		return v.(time.Duration)
	}
	return 0
}

func itoa(n int) string { return strconv.Itoa(n) }

// execute claims the post (from the given statuses) and publishes it once.
// If another caller holds the claim it returns the post as it stands.
func (s *Server) execute(ctx context.Context, p store.Post, from ...string) (store.Post, error) {
	claimed, err := s.Store.Claim(ctx, p.ID, from...)
	if err != nil {
		return p, err
	}
	if !claimed {
		return s.Store.Post(ctx, p.OrgID, p.ID)
	}
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
	defer cancel()

	ids, url, perr := s.publish(pubCtx, p)
	if perr == nil {
		err = s.Store.MarkPublished(ctx, p.ID, url, ids)
	} else {
		var e *providers.Error
		if !errors.As(perr, &e) {
			e = &providers.Error{Code: providers.CodeProviderUnavailable, Message: "publishing failed unexpectedly"}
		}
		if e.Code == providers.CodeRateLimited {
			retryAfters.Store(p.ID, e.RetryAfter)
		}
		if e.Code == providers.CodeAuthFailed && e.HTTPStatus == 401 {
			_ = s.Store.SetAccountStatus(ctx, p.AccountID, "error")
		}
		s.Log.Warn("publish failed", "post", p.ID, "platform", p.Platform, "code", e.Code, "attempt", p.Attempts+1)
		err = s.Store.MarkFailed(ctx, p.ID, e.Code, e.Message)
	}
	if err != nil {
		return p, err
	}
	return s.Store.Post(ctx, p.OrgID, p.ID)
}

func (s *Server) publish(ctx context.Context, p store.Post) (ids []string, url string, err error) {
	acc, err := s.Store.Account(ctx, p.OrgID, p.AccountID)
	if err != nil {
		return nil, "", providers.Errf(providers.CodeNotFound, "the account for this post no longer exists")
	}
	switch acc.Status {
	case "revoked":
		return nil, "", providers.Errf(providers.CodeRevoked, "the account was disconnected")
	case "error":
		return nil, "", providers.Errf(providers.CodeAuthFailed, "the account needs to be reconnected")
	}
	if acc.TokenExpiresAt != nil && time.Now().After(*acc.TokenExpiresAt) {
		_ = s.Store.SetAccountStatus(ctx, acc.ID, "error")
		return nil, "", providers.Errf(providers.CodeAuthFailed, "the %s access token expired; reconnect the account", acc.Platform)
	}
	token, err := s.Sealer.Open(acc.SealedToken)
	if err != nil {
		return nil, "", providers.Errf(providers.CodeAuthFailed, "the stored token cannot be read; reconnect the account")
	}

	switch p.Platform {
	case "linkedin":
		if len(p.ExternalIDs) > 0 { // already posted by an earlier attempt
			return p.ExternalIDs, "https://www.linkedin.com/feed/update/" + p.ExternalIDs[0] + "/", nil
		}
		urn, u, err := s.LinkedIn.Post(ctx, token, acc.ExternalID, p.Text)
		if err != nil {
			return nil, "", err
		}
		if urn == "" {
			return []string{}, "", nil
		}
		return []string{urn}, u, nil
	case "x":
		tok, sec, ok := cut(token)
		if !ok {
			return nil, "", providers.Errf(providers.CodeAuthFailed, "the stored X token is incomplete; reconnect the account")
		}
		parts := providers.SplitThread(p.Text)
		ids, err := s.X.PostThread(ctx, tok, sec, parts, p.ExternalIDs, func(done []string) error {
			return s.Store.SetExternalIDs(context.WithoutCancel(ctx), p.ID, done)
		})
		if err != nil {
			return ids, "", err
		}
		return ids, providers.PostURL(ids[0]), nil
	}
	return nil, "", providers.Errf(providers.CodeUnsupportedFormat, "unsupported channel %s", p.Platform)
}

func cut(token string) (string, string, bool) {
	for i := 0; i < len(token); i++ {
		if token[i] == '|' {
			return token[:i], token[i+1:], token[:i] != "" && token[i+1:] != ""
		}
	}
	return "", "", false
}

// RunScheduler publishes due scheduled posts until ctx ends. A scheduled post
// that fails is final (status failed); the caller decides whether to retry.
func (s *Server) RunScheduler(ctx context.Context, every time.Duration) {
	if n, err := s.Store.RecoverStuck(ctx, 0); err == nil && n > 0 {
		s.Log.Info("recovered interrupted posts", "count", n)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick runs one scheduler pass.
func (s *Server) Tick(ctx context.Context) { s.TickAt(ctx, time.Now()) }

// TickAt publishes posts due at or before at.
func (s *Server) TickAt(ctx context.Context, at time.Time) {
	due, err := s.Store.DuePosts(ctx, at)
	if err != nil {
		s.Log.Error("scheduler", "err", err)
		return
	}
	for _, p := range due {
		if _, err := s.execute(ctx, p, "scheduled"); err != nil {
			s.Log.Error("scheduler publish", "post", p.ID, "err", err)
		}
	}
}
