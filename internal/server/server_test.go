package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kael-tombo/socialflow-lite/internal/providers"
	"github.com/kael-tombo/socialflow-lite/internal/store"
)

// fake plays LinkedIn and X on one httptest server.
type fake struct {
	mu            sync.Mutex
	linkedInPosts []map[string]any
	linkedInHdrs  []http.Header
	tweets        []map[string]any
	tweetCalls    int
	failTweetCall int // 1-based call number that returns 503 once
	liStatus      int // when set, /rest/posts answers this status
	liRetryAfter  string
}

func (f *fake) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		switch r.Method + " " + r.URL.Path {
		case "POST /oauth/v2/accessToken":
			_ = r.ParseForm()
			if !strings.Contains(string(body), "code=good-code") {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`{"access_token":"li-token","expires_in":5184000}`))
		case "GET /v2/userinfo":
			if r.Header.Get("Authorization") != "Bearer li-token" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"sub":"abc123","name":"Ada Founder","given_name":"Ada"}`))
		case "POST /rest/posts":
			if f.liStatus != 0 {
				if f.liRetryAfter != "" {
					w.Header().Set("Retry-After", f.liRetryAfter)
				}
				w.WriteHeader(f.liStatus)
				w.Write([]byte(`{"message":"simulated"}`))
				return
			}
			var p map[string]any
			_ = json.Unmarshal(body, &p)
			f.linkedInPosts = append(f.linkedInPosts, p)
			f.linkedInHdrs = append(f.linkedInHdrs, r.Header.Clone())
			w.Header().Set("x-restli-id", "urn:li:share:700"+itoa(len(f.linkedInPosts)))
			w.WriteHeader(201)
		case "POST /oauth/v2/revoke", "POST /1.1/oauth/invalidate_token":
			w.WriteHeader(200)
		case "POST /oauth/request_token":
			if !strings.Contains(r.Header.Get("Authorization"), "oauth_callback=") {
				t.Errorf("request_token without oauth_callback")
			}
			w.Write([]byte("oauth_token=rt&oauth_token_secret=rts&oauth_callback_confirmed=true"))
		case "POST /oauth/access_token":
			a := r.Header.Get("Authorization")
			if !strings.Contains(a, `oauth_verifier="v1"`) || !strings.Contains(a, `oauth_token="rt"`) {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte("oauth_token=at&oauth_token_secret=ats&user_id=42&screen_name=ada"))
		case "GET /2/users/me":
			if !strings.Contains(r.Header.Get("Authorization"), `oauth_token="at"`) {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"data":{"id":"42","name":"Ada","username":"ada"}}`))
		case "POST /2/tweets":
			f.tweetCalls++
			if f.tweetCalls == f.failTweetCall {
				w.WriteHeader(503)
				return
			}
			var p map[string]any
			_ = json.Unmarshal(body, &p)
			f.tweets = append(f.tweets, p)
			w.WriteHeader(201)
			w.Write([]byte(`{"data":{"id":"t` + itoa(len(f.tweets)) + `","text":"x"}}`))
		default:
			t.Errorf("unexpected provider call %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})
}

type env struct {
	t    *testing.T
	srv  *Server
	api  *httptest.Server
	fake *fake
	key  string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	f := &fake{}
	prov := httptest.NewServer(f.handler(t))
	t.Cleanup(prov.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sealer, _ := store.NewSealer(strings.Repeat("ab", 32))
	s := &Server{
		Store: st, Sealer: sealer, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		LinkedIn: &providers.LinkedIn{ClientID: "cid", ClientSecret: "csec", AuthBase: prov.URL, APIBase: prov.URL, HTTP: prov.Client()},
		X:        &providers.X{ConsumerKey: "ck", ConsumerSecret: "cs", APIBase: prov.URL, HTTP: prov.Client()},
	}
	api := httptest.NewServer(s.Handler())
	t.Cleanup(api.Close)
	s.PublicURL = api.URL
	s.LinkedIn.RedirectURL = api.URL + "/oauth/linkedin/callback"
	s.X.CallbackURL = api.URL + "/oauth/x/callback"
	_, key, err := st.CreateOrgWithKey(context.Background(), "Banyan")
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, srv: s, api: api, fake: f, key: key}
}

type resp struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func (e *env) call(method, path, key string, body any, hdr map[string]string) resp {
	e.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.api.URL+path, rdr)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	out := resp{status: r.StatusCode, header: r.Header, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	dump(e.t, method, path, out)
	return out
}

// dump writes contract responses for the Banyan zod check (SFL_CONTRACT_DUMP=dir).
func dump(t *testing.T, method, path string, r resp) {
	dir := os.Getenv("SFL_CONTRACT_DUMP")
	if dir == "" || !strings.HasPrefix(path, "/v1/") || r.body == nil {
		return
	}
	name := strings.NewReplacer("/", "_", "?", "_").Replace(t.Name() + "_" + method + path)
	rec, _ := json.Marshal(map[string]any{"method": method, "path": path, "status": r.status, "body": r.body})
	_ = os.WriteFile(filepath.Join(dir, name+"_"+itoa(int(time.Now().UnixNano()%1e9))+".json"), rec, 0o644)
}

func errCode(r resp) string {
	if e, ok := r.body["error"].(map[string]any); ok {
		s, _ := e["code"].(string)
		return s
	}
	return ""
}

func (e *env) connectLinkedIn() {
	e.t.Helper()
	r := e.call("POST", "/v1/accounts/connect", e.key, map[string]string{"platform": "linkedin"}, nil)
	if r.status != 201 {
		e.t.Fatalf("connect: %d %s", r.status, r.raw)
	}
	cu, _ := url.Parse(r.body["connect_url"].(string))
	red := e.call("GET", cu.Path, "", nil, nil)
	if red.status != 302 {
		e.t.Fatalf("connect redirect: %d", red.status)
	}
	loc, _ := url.Parse(red.header.Get("Location"))
	if loc.Path != "/oauth/v2/authorization" || loc.Query().Get("scope") != "openid profile w_member_social" {
		e.t.Fatalf("unexpected LinkedIn auth URL %s", loc)
	}
	cb := e.call("GET", "/oauth/linkedin/callback?code=good-code&state="+loc.Query().Get("state"), "", nil, nil)
	if cb.status != 200 || !strings.Contains(cb.raw, "Connected Ada Founder") {
		e.t.Fatalf("callback: %d %s", cb.status, cb.raw)
	}
	again := e.call("GET", "/oauth/linkedin/callback?code=good-code&state="+loc.Query().Get("state"), "", nil, nil)
	if again.status != 410 {
		e.t.Fatalf("reused callback should be refused, got %d", again.status)
	}
}

func (e *env) connectX() {
	e.t.Helper()
	r := e.call("POST", "/v1/accounts/connect", e.key, map[string]string{"platform": "x"}, nil)
	cu, _ := url.Parse(r.body["connect_url"].(string))
	red := e.call("GET", cu.Path, "", nil, nil)
	if red.status != 302 || !strings.Contains(red.header.Get("Location"), "/oauth/authorize?oauth_token=rt") {
		e.t.Fatalf("x redirect: %d %s", red.status, red.header.Get("Location"))
	}
	cb := e.call("GET", "/oauth/x/callback?oauth_token=rt&oauth_verifier=v1", "", nil, nil)
	if cb.status != 200 || !strings.Contains(cb.raw, "Connected @ada") {
		e.t.Fatalf("x callback: %d %s", cb.status, cb.raw)
	}
}

// ───────── tests ─────────

func TestAuthAndMe(t *testing.T) {
	e := newEnv(t)
	if r := e.call("GET", "/v1/me", "", nil, nil); r.status != 401 || errCode(r) != "auth_failed" {
		t.Fatalf("no key: %d %s", r.status, r.raw)
	}
	if r := e.call("GET", "/v1/me", "sfl_wrong", nil, nil); r.status != 401 {
		t.Fatalf("bad key: %d", r.status)
	}
	r := e.call("GET", "/v1/me", e.key, nil, nil)
	if r.status != 200 || !strings.HasPrefix(r.body["organization_id"].(string), "org_") {
		t.Fatalf("me: %d %s", r.status, r.raw)
	}
}

func TestLinkedInConnectPublishAndIdempotency(t *testing.T) {
	e := newEnv(t)
	e.connectLinkedIn()

	acc := e.call("GET", "/v1/accounts", e.key, nil, nil)
	list := acc.body["accounts"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["status"] != "active" || list[0].(map[string]any)["platform"] != "linkedin" {
		t.Fatalf("accounts: %s", acc.raw)
	}
	accID := list[0].(map[string]any)["id"].(string)
	if c := e.call("GET", "/v1/accounts/"+accID+"/capabilities", e.key, nil, nil); c.body["publish"] != true || c.body["analytics"] != false {
		t.Fatalf("capabilities: %s", c.raw)
	}

	body := map[string]string{"channel": "linkedin", "text": "We shipped API usage alerts (beta) #launch"}
	hdr := map[string]string{"Idempotency-Key": "pub-1"}
	r := e.call("POST", "/v1/posts", e.key, body, hdr)
	if r.status != 201 || r.body["status"] != "published" || r.body["url"] != "https://www.linkedin.com/feed/update/urn:li:share:7001/" {
		t.Fatalf("publish: %d %s", r.status, r.raw)
	}
	if got := e.fake.linkedInPosts[0]["commentary"]; got != `We shipped API usage alerts \(beta\) {hashtag|\#|launch}` {
		t.Fatalf("commentary = %v", got)
	}
	if h := e.fake.linkedInHdrs[0]; h.Get("X-Restli-Protocol-Version") != "2.0.0" || len(h.Get("Linkedin-Version")) != 6 {
		t.Fatalf("LinkedIn headers: %v", h)
	}
	if e.fake.linkedInPosts[0]["author"] != "urn:li:person:abc123" {
		t.Fatalf("author = %v", e.fake.linkedInPosts[0]["author"])
	}

	replay := e.call("POST", "/v1/posts", e.key, body, hdr)
	if replay.status != 200 || replay.body["id"] != r.body["id"] || len(e.fake.linkedInPosts) != 1 {
		t.Fatalf("replay must return the same post without reposting: %d %s (posts=%d)", replay.status, replay.raw, len(e.fake.linkedInPosts))
	}
	clash := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "different"}, hdr)
	if clash.status != 422 || errCode(clash) != "validation" {
		t.Fatalf("reused key with new body: %d %s", clash.status, clash.raw)
	}
	got := e.call("GET", "/v1/posts/"+r.body["id"].(string), e.key, nil, nil)
	if got.body["status"] != "published" {
		t.Fatalf("status: %s", got.raw)
	}
	if a := e.call("GET", "/v1/posts/"+r.body["id"].(string)+"/analytics", e.key, nil, nil); a.status != 404 || errCode(a) != "not_found" {
		t.Fatalf("analytics should be honestly unsupported: %d %s", a.status, a.raw)
	}
}

func TestXThreadResumesAfterMidwayFailure(t *testing.T) {
	e := newEnv(t)
	e.connectX()
	e.fake.failTweetCall = 2
	body := map[string]string{"channel": "x", "text": "1/ We shipped usage alerts.\n\n2/ They fire before you hit a limit.\n\n3/ Try it: https://example.com/alerts"}
	hdr := map[string]string{"Idempotency-Key": "thread-1"}

	first := e.call("POST", "/v1/posts", e.key, body, hdr)
	if first.status != 502 || errCode(first) != "provider_unavailable" || !strings.Contains(first.raw, "posted 1 of 3") {
		t.Fatalf("midway failure should be retryable: %d %s", first.status, first.raw)
	}
	retry := e.call("POST", "/v1/posts", e.key, body, hdr)
	if retry.status != 200 || retry.body["status"] != "published" || retry.body["url"] != "https://x.com/i/web/status/t1" {
		t.Fatalf("retry: %d %s", retry.status, retry.raw)
	}
	tw := e.fake.tweets
	if len(tw) != 3 {
		t.Fatalf("want exactly 3 tweets (no duplicate of the first), got %d", len(tw))
	}
	if _, ok := tw[0]["reply"]; ok {
		t.Fatal("first tweet must not be a reply")
	}
	if tw[1]["reply"].(map[string]any)["in_reply_to_tweet_id"] != "t1" || tw[2]["reply"].(map[string]any)["in_reply_to_tweet_id"] != "t2" {
		t.Fatalf("thread chaining wrong: %v", tw)
	}
}

func TestRateLimitIsSignalledWithRetryAfter(t *testing.T) {
	e := newEnv(t)
	e.connectLinkedIn()
	e.fake.liStatus, e.fake.liRetryAfter = 429, "7"
	r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "hello"}, map[string]string{"Idempotency-Key": "rl"})
	if r.status != 429 || errCode(r) != "rate_limited" || r.header.Get("Retry-After") < "7" {
		t.Fatalf("rate limit: %d %s retry-after=%q", r.status, r.raw, r.header.Get("Retry-After"))
	}
	e.fake.liStatus = 0
	ok := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "hello"}, map[string]string{"Idempotency-Key": "rl"})
	if ok.body["status"] != "published" {
		t.Fatalf("retry after rate limit: %s", ok.raw)
	}
}

func TestRejectedTokenMarksAccountForReconnect(t *testing.T) {
	e := newEnv(t)
	e.connectLinkedIn()
	e.fake.liStatus = 401
	r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "hello"}, nil)
	if r.status != 201 || r.body["status"] != "failed" || errCode(r) != "auth_failed" {
		t.Fatalf("401: %d %s", r.status, r.raw)
	}
	acc := e.call("GET", "/v1/accounts", e.key, nil, nil)
	if a := acc.body["accounts"].([]any)[0].(map[string]any); a["status"] != "error" || a["capabilities"].(map[string]any)["publish"] != false {
		t.Fatalf("account should need reconnect: %s", acc.raw)
	}
	next := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "again"}, nil)
	if next.status != 409 || errCode(next) != "auth_failed" {
		t.Fatalf("publishing to an account needing reconnect: %d %s", next.status, next.raw)
	}
	// Reconnecting restores it.
	e.fake.liStatus = 0
	e.connectLinkedIn()
	if again := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "again"}, nil); again.body["status"] != "published" {
		t.Fatalf("after reconnect: %s", again.raw)
	}
}

func TestValidationAndUnsupported(t *testing.T) {
	e := newEnv(t)
	e.connectX()
	if r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "shortvideo", "text": "script"}, nil); r.status != 422 || errCode(r) != "unsupported_format" {
		t.Fatalf("shortvideo: %d %s", r.status, r.raw)
	}
	if r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "x", "text": strings.Repeat("a", 281)}, nil); r.status != 422 || errCode(r) != "unsupported_format" {
		t.Fatalf("long tweet: %d %s", r.status, r.raw)
	}
	if r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "no account"}, nil); r.status != 422 || errCode(r) != "validation" {
		t.Fatalf("no linkedin account: %d %s", r.status, r.raw)
	}
	if r := e.call("POST", "/v1/accounts/connect", e.key, map[string]string{"platform": "tiktok"}, nil); errCode(r) != "unsupported_format" {
		t.Fatalf("connect tiktok: %s", r.raw)
	}
	if len(e.fake.tweets) != 0 {
		t.Fatal("nothing may be posted on validation failures")
	}
}

func TestScheduledPostPublishesWhenDue(t *testing.T) {
	e := newEnv(t)
	e.connectLinkedIn()
	at := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "later", "scheduled_at": at}, map[string]string{"Idempotency-Key": "s1"})
	if r.status != 201 || r.body["status"] != "scheduled" || len(e.fake.linkedInPosts) != 0 {
		t.Fatalf("schedule: %d %s", r.status, r.raw)
	}
	e.srv.TickAt(context.Background(), time.Now())
	if len(e.fake.linkedInPosts) != 0 {
		t.Fatal("published before its time")
	}
	e.srv.TickAt(context.Background(), time.Now().Add(2*time.Hour))
	got := e.call("GET", "/v1/posts/"+r.body["id"].(string), e.key, nil, nil)
	if got.body["status"] != "published" || len(e.fake.linkedInPosts) != 1 {
		t.Fatalf("after due tick: %s", got.raw)
	}
	e.srv.TickAt(context.Background(), time.Now().Add(3*time.Hour))
	if len(e.fake.linkedInPosts) != 1 {
		t.Fatal("scheduler published twice")
	}
}

func TestRevokeAndOrgIsolation(t *testing.T) {
	e := newEnv(t)
	e.connectLinkedIn()
	post := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "mine"}, nil)
	accID := e.call("GET", "/v1/accounts", e.key, nil, nil).body["accounts"].([]any)[0].(map[string]any)["id"].(string)

	_, otherKey, _ := e.srv.Store.CreateOrgWithKey(context.Background(), "Other")
	if r := e.call("GET", "/v1/posts/"+post.body["id"].(string), otherKey, nil, nil); r.status != 404 {
		t.Fatalf("another org must not see the post: %d", r.status)
	}
	if r := e.call("DELETE", "/v1/accounts/"+accID, otherKey, nil, nil); r.status != 404 {
		t.Fatalf("another org must not revoke the account: %d", r.status)
	}
	if r := e.call("POST", "/v1/posts", otherKey, map[string]string{"channel": "linkedin", "text": "x", "account_id": accID}, nil); r.status != 404 {
		t.Fatalf("another org must not post as the account: %d %s", r.status, r.raw)
	}

	rv := e.call("DELETE", "/v1/accounts/"+accID, e.key, nil, nil)
	if rv.status != 200 || rv.body["status"] != "revoked" {
		t.Fatalf("revoke: %d %s", rv.status, rv.raw)
	}
	if r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "after", "account_id": accID}, nil); r.status != 409 || errCode(r) != "revoked" {
		t.Fatalf("post after revoke: %d %s", r.status, r.raw)
	}
}

func TestConcurrentSameKeyPublishesOnce(t *testing.T) {
	e := newEnv(t)
	e.connectLinkedIn()
	body := map[string]string{"channel": "linkedin", "text": "once"}
	var wg sync.WaitGroup
	statuses := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := e.call("POST", "/v1/posts", e.key, body, map[string]string{"Idempotency-Key": "race"})
			statuses <- r.body["status"].(string)
		}()
	}
	wg.Wait()
	close(statuses)
	for s := range statuses {
		if s != "published" && s != "queued" {
			t.Fatalf("unexpected status %q", s)
		}
	}
	if n := len(e.fake.linkedInPosts); n != 1 {
		t.Fatalf("10 concurrent requests with one key produced %d posts", n)
	}
}

func TestTokenExpiryIsReported(t *testing.T) {
	e := newEnv(t)
	e.connectLinkedIn()
	e.connectX()
	list := e.call("GET", "/v1/accounts", e.key, nil, nil).body["accounts"].([]any)
	var li, x map[string]any
	for _, a := range list {
		m := a.(map[string]any)
		if m["platform"] == "linkedin" {
			li = m
		} else {
			x = m
		}
	}
	exp, err := time.Parse(time.RFC3339Nano, li["token_expires_at"].(string))
	if err != nil || exp.Before(time.Now().Add(59*24*time.Hour)) || exp.After(time.Now().Add(61*24*time.Hour)) {
		t.Fatalf("LinkedIn expiry should be ~60 days out, got %v (%v)", li["token_expires_at"], err)
	}
	if x["token_expires_at"] != nil {
		t.Fatalf("X tokens do not expire, got %v", x["token_expires_at"])
	}

	// A token past its expiry is reported as needing reconnection immediately.
	sealed, _ := e.srv.Sealer.Seal("li-token")
	if _, err := e.srv.Store.UpsertAccount(context.Background(), e.orgID(), "linkedin", "abc123", "Ada Founder", sealed, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, a := range e.call("GET", "/v1/accounts", e.key, nil, nil).body["accounts"].([]any) {
		m := a.(map[string]any)
		if m["platform"] == "linkedin" && (m["status"] != "error" || m["capabilities"].(map[string]any)["publish"] != false) {
			t.Fatalf("expired LinkedIn account must report error: %v", m)
		}
	}
}

func (e *env) orgID() string {
	return e.call("GET", "/v1/me", e.key, nil, nil).body["organization_id"].(string)
}

func TestCancelScheduledPost(t *testing.T) {
	e := newEnv(t)
	e.connectLinkedIn()
	at := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	body := map[string]string{"channel": "linkedin", "text": "later", "scheduled_at": at}
	hdr := map[string]string{"Idempotency-Key": "c1"}
	s := e.call("POST", "/v1/posts", e.key, body, hdr)
	id := s.body["id"].(string)

	c := e.call("DELETE", "/v1/posts/"+id, e.key, nil, nil)
	if c.status != 200 || c.body["status"] != "cancelled" {
		t.Fatalf("cancel: %d %s", c.status, c.raw)
	}
	if again := e.call("DELETE", "/v1/posts/"+id, e.key, nil, nil); again.status != 200 || again.body["status"] != "cancelled" {
		t.Fatalf("cancelling twice must be idempotent: %d %s", again.status, again.raw)
	}
	e.srv.TickAt(context.Background(), time.Now().Add(2*time.Hour))
	if len(e.fake.linkedInPosts) != 0 {
		t.Fatal("a cancelled post must never be published by the scheduler")
	}
	if replay := e.call("POST", "/v1/posts", e.key, body, hdr); replay.body["status"] != "cancelled" || len(e.fake.linkedInPosts) != 0 {
		t.Fatalf("replaying the original request must not resurrect it: %s", replay.raw)
	}

	_, otherKey, _ := e.srv.Store.CreateOrgWithKey(context.Background(), "Other")
	if r := e.call("DELETE", "/v1/posts/"+id, otherKey, nil, nil); r.status != 404 {
		t.Fatalf("another org must get 404, got %d", r.status)
	}
}

func TestCancelPublishedPostIsRefused(t *testing.T) {
	e := newEnv(t)
	e.connectLinkedIn()
	p := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "now"}, map[string]string{"Idempotency-Key": "n1"})
	r := e.call("DELETE", "/v1/posts/"+p.body["id"].(string), e.key, nil, nil)
	if r.status != 409 || errCode(r) != "validation" || !strings.Contains(r.raw, "already published") {
		t.Fatalf("cancelling a published post: %d %s", r.status, r.raw)
	}
	if status, body, _ := e.tool("cancel_post", map[string]any{"post_id": p.body["id"]}); status != 409 || body["error"] == nil {
		t.Fatalf("MCP cancel_post on a published post: %d %v", status, body)
	}
}
