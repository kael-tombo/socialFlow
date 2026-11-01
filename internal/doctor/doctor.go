// Package doctor checks a SocialFlow Lite configuration against the real
// providers without needing a user to sign in: it prints what to enter in
// each developer console, then proves the credentials that came back work.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kael-tombo/socialflow-lite/internal/providers"
)

type Status string

const (
	Pass Status = "PASS"
	Fail Status = "FAIL"
	Warn Status = "WARN"
	Info Status = "INFO"
)

type Result struct {
	Status Status
	Name   string
	Detail string
}

type Config struct {
	PublicURL      string
	EncryptionOK   bool
	EncryptionErr  string
	DatabaseOK     bool
	DatabaseErr    string
	LinkedIn       *providers.LinkedIn
	X              *providers.X
	HTTP           *http.Client
	SkipLiveChecks bool
}

// Run returns every check, in the order a developer fixes them.
func Run(ctx context.Context, c Config) []Result {
	var out []Result
	add := func(s Status, name, detail string) { out = append(out, Result{s, name, detail}) }

	// ── this service ──
	u, err := url.Parse(c.PublicURL)
	switch {
	case c.PublicURL == "" || err != nil || u.Host == "":
		add(Fail, "PUBLIC_URL", "not set or not a URL; the provider callbacks are built from it")
	case u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1":
		add(Fail, "PUBLIC_URL", "must be https outside localhost")
	case u.Scheme != "https":
		add(Warn, "PUBLIC_URL", c.PublicURL+" is local: LinkedIn and X can call back to it only from this machine")
	default:
		add(Pass, "PUBLIC_URL", c.PublicURL)
	}
	if c.EncryptionOK {
		add(Pass, "ENCRYPTION_KEY", "valid 32-byte key")
	} else {
		add(Fail, "ENCRYPTION_KEY", c.EncryptionErr)
	}
	if c.DatabaseOK {
		add(Pass, "DATABASE_PATH", "opened and migrated")
	} else {
		add(Fail, "DATABASE_PATH", c.DatabaseErr)
	}

	// ── LinkedIn ──
	add(Info, "LinkedIn console", strings.Join([]string{
		"Products: \"Share on LinkedIn\" and \"Sign In with LinkedIn using OpenID Connect\"",
		"Auth → Authorized redirect URL: " + c.LinkedIn.RedirectURL,
		"Scopes requested: openid profile w_member_social",
	}, "\n         "))
	if !c.LinkedIn.Configured() {
		add(Warn, "LinkedIn credentials", "LINKEDIN_CLIENT_ID / LINKEDIN_CLIENT_SECRET not set; LinkedIn stays unavailable")
	} else if c.SkipLiveChecks {
		add(Info, "LinkedIn credentials", "set (live check skipped)")
	} else {
		s, d := checkLinkedIn(ctx, c)
		add(s, "LinkedIn credentials", d)
	}

	// ── X ──
	add(Info, "X console", strings.Join([]string{
		"User authentication settings: OAuth 1.0a ON, App permissions \"Read and write\"",
		"Callback URI / Redirect URL: " + c.X.CallbackURL,
		"Website URL: any public page you own",
		"Keys and tokens → API Key and Secret go in X_CONSUMER_KEY / X_CONSUMER_SECRET",
	}, "\n         "))
	if !c.X.Configured() {
		add(Warn, "X credentials", "X_CONSUMER_KEY / X_CONSUMER_SECRET not set; X stays unavailable")
	} else if c.SkipLiveChecks {
		add(Info, "X credentials", "set (live check skipped)")
	} else {
		s, d := checkX(ctx, c)
		add(s, "X credentials", d)
	}
	return out
}

// checkLinkedIn exchanges a deliberately invalid code. LinkedIn rejects the
// code either way, but says "invalid_client" only when the app credentials
// are wrong — so any other rejection proves the client id and secret.
func checkLinkedIn(ctx context.Context, c Config) (Status, string) {
	base := strings.TrimRight(firstNonEmpty(c.LinkedIn.AuthBase, "https://www.linkedin.com"), "/")
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {"socialflow-lite-doctor"},
		"redirect_uri": {c.LinkedIn.RedirectURL}, "client_id": {c.LinkedIn.ClientID}, "client_secret": {c.LinkedIn.ClientSecret},
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/oauth/v2/accessToken", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Fail, "could not reach LinkedIn: " + err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	text := strings.ToLower(string(body))
	switch {
	case strings.Contains(text, "invalid_client") || resp.StatusCode == http.StatusUnauthorized:
		return Fail, "LinkedIn rejected the client id or secret (invalid_client). Copy them again from the app's Auth tab."
	case resp.StatusCode >= 500:
		return Warn, fmt.Sprintf("LinkedIn answered %d; try again later", resp.StatusCode)
	case resp.StatusCode == http.StatusBadRequest:
		return Pass, "client id and secret accepted (LinkedIn refused only the test code, as expected). " +
			"The redirect URL itself is checked when you first connect an account."
	default:
		return Warn, fmt.Sprintf("unexpected LinkedIn response %d; could not confirm the credentials", resp.StatusCode)
	}
}

// checkX asks for a real OAuth 1.0a request token, which proves the API key
// and secret, that OAuth 1.0a is enabled, and that the callback URL is
// registered exactly as this service will send it.
func checkX(ctx context.Context, c Config) (Status, string) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, _, _, err := c.X.RequestToken(ctx)
	if err == nil {
		return Pass, "API key and secret accepted, OAuth 1.0a enabled, callback URL registered"
	}
	var e *providers.Error
	if errors.As(err, &e) {
		switch {
		case e.HTTPStatus == http.StatusUnauthorized || e.HTTPStatus == http.StatusForbidden:
			return Fail, "X refused the request token (" + e.Message + "). Usual causes: wrong API key/secret, OAuth 1.0a not enabled, " +
				"or the callback URL not registered exactly as " + c.X.CallbackURL
		case e.Code == providers.CodeProviderUnavailable:
			return Warn, "X did not answer: " + e.Message
		}
		return Fail, e.Message
	}
	return Fail, err.Error()
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// Failed reports whether any check failed.
func Failed(rs []Result) bool {
	for _, r := range rs {
		if r.Status == Fail {
			return true
		}
	}
	return false
}
