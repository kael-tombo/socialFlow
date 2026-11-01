package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kael-tombo/socialflow-lite/internal/oauth1"
)

// X publishes posts and threads with OAuth 1.0a user context, as upstream
// does. Requires an X developer app with read+write permission and the
// callback URL registered.
type X struct {
	ConsumerKey    string
	ConsumerSecret string
	CallbackURL    string
	APIBase        string // https://api.twitter.com
	HTTP           *http.Client
}

const XMaxLength = 280

func (x *X) Configured() bool { return x.ConsumerKey != "" && x.ConsumerSecret != "" }

func (x *X) Capabilities() Capabilities {
	return Capabilities{Publish: true, Schedule: true, Analytics: false, Formats: []string{"post", "thread"}, MaxLength: XMaxLength}
}

func (x *X) base() string {
	return strings.TrimRight(firstNonEmpty(x.APIBase, "https://api.twitter.com"), "/")
}

func (x *X) signed(ctx context.Context, method, rawURL string, c oauth1.Credentials, extra map[string]string, body io.Reader, contentType string) ([]byte, error) {
	auth, err := oauth1.Header(method, rawURL, c, extra, nil, oauth1.Nonce(), time.Now())
	if err != nil {
		return nil, Errf(CodeValidation, "bad X request URL")
	}
	req, _ := http.NewRequestWithContext(ctx, method, rawURL, body)
	req.Header.Set("Authorization", auth)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	b, _, err := do(x.HTTP, req, "X")
	return b, err
}

func (x *X) app() oauth1.Credentials {
	return oauth1.Credentials{ConsumerKey: x.ConsumerKey, ConsumerSecret: x.ConsumerSecret}
}

// RequestToken starts the 3-legged flow and returns where to send the user.
func (x *X) RequestToken(ctx context.Context) (token, secret, authURL string, err error) {
	body, err := x.signed(ctx, http.MethodPost, x.base()+"/oauth/request_token", x.app(), map[string]string{"oauth_callback": x.CallbackURL}, nil, "")
	if err != nil {
		return "", "", "", err
	}
	v, _ := url.ParseQuery(string(body))
	if v.Get("oauth_token") == "" || v.Get("oauth_callback_confirmed") != "true" {
		return "", "", "", Errf(CodeAuthFailed, "X did not issue a request token; check the app's callback URL")
	}
	return v.Get("oauth_token"), v.Get("oauth_token_secret"), x.base() + "/oauth/authorize?oauth_token=" + url.QueryEscape(v.Get("oauth_token")), nil
}

// AccessToken completes the flow.
func (x *X) AccessToken(ctx context.Context, token, secret, verifier string) (accessToken, accessSecret string, err error) {
	c := x.app()
	c.Token, c.TokenSecret = token, secret
	body, err := x.signed(ctx, http.MethodPost, x.base()+"/oauth/access_token", c, map[string]string{"oauth_verifier": verifier}, nil, "")
	if err != nil {
		return "", "", err
	}
	v, _ := url.ParseQuery(string(body))
	if v.Get("oauth_token") == "" || v.Get("oauth_token_secret") == "" {
		return "", "", Errf(CodeAuthFailed, "X did not issue an access token")
	}
	return v.Get("oauth_token"), v.Get("oauth_token_secret"), nil
}

func (x *X) user(tok, sec string) oauth1.Credentials {
	c := x.app()
	c.Token, c.TokenSecret = tok, sec
	return c
}

// Me reads the authenticated account.
func (x *X) Me(ctx context.Context, tok, sec string) (Profile, error) {
	body, err := x.signed(ctx, http.MethodGet, x.base()+"/2/users/me?user.fields=id,name,username", x.user(tok, sec), nil, nil, "")
	if err != nil {
		return Profile{}, err
	}
	var r struct {
		Data struct{ ID, Name, Username string } `json:"data"`
	}
	if json.Unmarshal(body, &r) != nil || r.Data.ID == "" {
		return Profile{}, Errf(CodeAuthFailed, "X profile response had no account id")
	}
	return Profile{ID: r.Data.ID, Handle: "@" + r.Data.Username, Name: r.Data.Name}, nil
}

// ValidateThread checks every part before anything is posted.
func ValidateThread(parts []string) *Error {
	for i, p := range parts {
		if p == "" {
			return Errf(CodeValidation, "post %d of the thread is empty", i+1)
		}
		if n := XLength(p); n > XMaxLength {
			return Errf(CodeUnsupportedFormat, "post %d of the thread is %d characters; X allows %d", i+1, n, XMaxLength)
		}
	}
	return nil
}

// PostThread posts parts in order, each replying to the previous one. done
// holds ids already posted by an earlier attempt, so a retry resumes rather
// than reposting; progress is called after every successful post.
func (x *X) PostThread(ctx context.Context, tok, sec string, parts, done []string, progress func([]string) error) ([]string, error) {
	ids := append([]string(nil), done...)
	for i := len(ids); i < len(parts); i++ {
		payload := map[string]any{"text": parts[i]}
		if i > 0 {
			payload["reply"] = map[string]string{"in_reply_to_tweet_id": ids[i-1]}
		}
		raw, _ := json.Marshal(payload)
		body, err := x.signed(ctx, http.MethodPost, x.base()+"/2/tweets", x.user(tok, sec), nil, strings.NewReader(string(raw)), "application/json")
		if err != nil {
			if e, ok := err.(*Error); ok && i > 0 {
				e.Message = "posted " + strconv.Itoa(i) + " of " + strconv.Itoa(len(parts)) + " thread posts; " + e.Message
			}
			return ids, err
		}
		var r struct {
			Data struct{ ID string } `json:"data"`
		}
		if json.Unmarshal(body, &r) != nil || r.Data.ID == "" {
			return ids, Errf(CodeProviderUnavailable, "X accepted a post but returned no id")
		}
		ids = append(ids, r.Data.ID)
		if progress != nil {
			if err := progress(ids); err != nil {
				return ids, Errf(CodeProviderUnavailable, "saving thread progress failed")
			}
		}
	}
	return ids, nil
}

// PostURL is X's handle-independent permalink.
func PostURL(id string) string { return "https://x.com/i/web/status/" + id }

// Revoke invalidates the user token at X (best effort).
func (x *X) Revoke(ctx context.Context, tok, sec string) error {
	_, err := x.signed(ctx, http.MethodPost, x.base()+"/1.1/oauth/invalidate_token", x.user(tok, sec), nil, nil, "")
	return err
}
