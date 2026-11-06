package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kael-tombo/socialflow-lite/internal/providers"
)

// Everything here guards one promise: a post in someone's name goes out once. Where that
// cannot be known, the service stops and says so rather than risking a second post.

// dropAfterReceiving makes the provider accept a request, then hang up without answering.
type dropAfterReceiving struct {
	inner http.Handler
	match string
	hits  int
	on    bool
}

func (d *dropAfterReceiving) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if d.on && r.URL.Path == d.match {
		d.hits++
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
		return
	}
	d.inner.ServeHTTP(w, r)
}

func connectedEnv(t *testing.T, platform string) *env {
	t.Helper()
	e := newEnv(t)
	switch platform {
	case "linkedin":
		e.connectLinkedIn()
	case "x":
		e.connectX()
	}
	return e
}

func postKey(k string) map[string]string { return map[string]string{"Idempotency-Key": k} }

func TestAKeyIsRequired(t *testing.T) {
	e := connectedEnv(t, "linkedin")
	r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "hello"}, nil)
	if r.status != 400 || len(e.fake.linkedInPosts) != 0 {
		t.Fatalf("posted without a key: %d %s", r.status, r.raw)
	}
}

func TestClientHangingUpDoesNotLoseThePublishedState(t *testing.T) {
	e := connectedEnv(t, "linkedin")
	r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "first", "scheduled_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}, postKey("hang"))
	if r.status != 201 {
		t.Fatalf("schedule: %d %s", r.status, r.raw)
	}
	post, err := e.srv.Store.PostByIdemKey(context.Background(), orgOf(t, e), "hang")
	if err != nil {
		t.Fatal(err)
	}

	// The caller goes away while the provider is handling the post, as when Banyan's 15 s timeout fires.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.fake.mu.Lock()
	e.fake.onLIPost = cancel
	e.fake.mu.Unlock()
	if _, err := e.srv.execute(ctx, post, "scheduled"); err != nil {
		t.Fatalf("a hung-up caller must not lose the outcome: %v", err)
	}
	again, err := e.srv.Store.Post(context.Background(), post.OrgID, post.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != "published" || again.URL == "" {
		t.Fatalf("status %q url %q; a retry after this would post twice", again.Status, again.URL)
	}
}

func TestDroppedConnectionAfterSendingStopsForAPerson(t *testing.T) {
	e := connectedEnv(t, "linkedin")
	// Route provider calls through a server that hangs up after receiving the post.
	drop := &dropAfterReceiving{inner: e.fake.handler(t), match: "/rest/posts", on: true}
	prov := httptest.NewServer(drop)
	defer prov.Close()
	e.srv.LinkedIn.APIBase, e.srv.LinkedIn.HTTP = prov.URL, prov.Client()

	r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "maybe"}, postKey("drop"))
	if r.status != 201 || r.body["status"] != "failed" {
		t.Fatalf("want a final failed post, got %d %s", r.status, r.raw)
	}
	errObj := r.body["error"].(map[string]any)
	if errObj["code"] != providers.CodeValidation {
		t.Fatalf("code %v must be one the contract does not auto-retry", errObj["code"])
	}
	if !strings.Contains(errObj["message"].(string), "may have gone out") {
		t.Fatalf("the message must say what happened: %v", errObj["message"])
	}

	// Banyan or a person replays the key. Nothing more may reach the provider.
	drop.on = false
	replay := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "maybe"}, postKey("drop"))
	if replay.status != 200 || replay.body["status"] != "failed" {
		t.Fatalf("replay: %d %s", replay.status, replay.raw)
	}
	if drop.hits != 1 || len(e.fake.linkedInPosts) != 0 {
		t.Fatalf("replay re-sent the post: hits=%d posts=%d", drop.hits, len(e.fake.linkedInPosts))
	}

	// A new key is the deliberate way to send it again.
	fresh := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "maybe"}, postKey("drop-2"))
	if fresh.body["status"] != "published" {
		t.Fatalf("new key: %d %s", fresh.status, fresh.raw)
	}
}

func TestProviderNeverReachedIsStillSafeToRetry(t *testing.T) {
	e := connectedEnv(t, "linkedin")
	realBase, realHTTP := e.srv.LinkedIn.APIBase, e.srv.LinkedIn.HTTP
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close() // connection refused: the request was never written
	e.srv.LinkedIn.APIBase = dead.URL

	r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "safe"}, postKey("refused"))
	if r.status != 502 {
		t.Fatalf("want a retryable 502, got %d %s", r.status, r.raw)
	}
	e.srv.LinkedIn.APIBase, e.srv.LinkedIn.HTTP = realBase, realHTTP
	retry := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "safe"}, postKey("refused"))
	if retry.body["status"] != "published" || len(e.fake.linkedInPosts) != 1 {
		t.Fatalf("retry: %d %s (posts %d)", retry.status, retry.raw, len(e.fake.linkedInPosts))
	}
}

func TestProviderAcceptingWithoutAnIDIsNotRetried(t *testing.T) {
	e := connectedEnv(t, "x")
	// X says yes but gives no id. It went out, so a retry would post it twice.
	calls := 0
	prov := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(201)
		w.Write([]byte(`{"data":{}}`))
	}))
	defer prov.Close()
	e.srv.X.APIBase, e.srv.X.HTTP = prov.URL, prov.Client()

	body := map[string]string{"channel": "x", "text": "no id"}
	if r := e.call("POST", "/v1/posts", e.key, body, postKey("noid")); r.body["status"] != "failed" {
		t.Fatalf("got %d %s", r.status, r.raw)
	}
	e.call("POST", "/v1/posts", e.key, body, postKey("noid"))
	if calls != 1 {
		t.Fatalf("X was called %d times", calls)
	}
}

func TestCrashDuringScheduledPublishIsNotRepublished(t *testing.T) {
	e := connectedEnv(t, "linkedin")
	ctx := context.Background()
	at := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	body := map[string]string{"channel": "linkedin", "text": "scheduled", "scheduled_at": at}
	e.call("POST", "/v1/posts", e.key, body, postKey("crash"))
	post, err := e.srv.Store.PostByIdemKey(ctx, orgOf(t, e), "crash")
	if err != nil {
		t.Fatal(err)
	}
	// The process claims the post, then dies before recording anything.
	if ok, err := e.srv.Store.Claim(ctx, post.ID, "scheduled"); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	time.Sleep(2 * time.Millisecond)
	if n, err := e.srv.Store.RecoverStuck(ctx, 0); err != nil || n != 1 {
		t.Fatalf("recover: %d %v", n, err)
	}

	e.srv.TickAt(ctx, time.Now().Add(2*time.Hour))
	replay := e.call("POST", "/v1/posts", e.key, body, postKey("crash"))
	if len(e.fake.linkedInPosts) != 0 {
		t.Fatalf("an interrupted post went out again with nobody deciding (%d posts)", len(e.fake.linkedInPosts))
	}
	if replay.body["status"] != "failed" || !strings.Contains(replay.raw, "may have gone out") {
		t.Fatalf("replay: %d %s", replay.status, replay.raw)
	}
}

func TestReplayAnswersFromStoredPostAfterDisconnect(t *testing.T) {
	e := connectedEnv(t, "linkedin")
	body := map[string]string{"channel": "linkedin", "text": "live"}
	first := e.call("POST", "/v1/posts", e.key, body, postKey("gone"))
	if first.body["status"] != "published" {
		t.Fatalf("publish: %s", first.raw)
	}
	accounts := e.call("GET", "/v1/accounts", e.key, nil, nil).body["accounts"].([]any)
	id := accounts[0].(map[string]any)["id"].(string)
	if r := e.call("DELETE", "/v1/accounts/"+id, e.key, nil, nil); r.status != 200 {
		t.Fatalf("disconnect: %d %s", r.status, r.raw)
	}

	replay := e.call("POST", "/v1/posts", e.key, body, postKey("gone"))
	if replay.status != 200 || replay.body["status"] != "published" {
		t.Fatalf("a published post must stay published on replay, got %d %s", replay.status, replay.raw)
	}
	if len(e.fake.linkedInPosts) != 1 {
		t.Fatalf("posts: %d", len(e.fake.linkedInPosts))
	}
	// A different body under the same key is still refused.
	if r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "other"}, postKey("gone")); r.status != 422 {
		t.Fatalf("different body: %d %s", r.status, r.raw)
	}
}

func TestScheduledPostRetriesARateLimitThenPublishes(t *testing.T) {
	e := connectedEnv(t, "linkedin")
	ctx := context.Background()
	at := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "soon", "scheduled_at": at}, postKey("rl-sched"))
	id := r.body["id"].(string)

	e.fake.mu.Lock()
	e.fake.liStatus, e.fake.liRetryAfter = 429, "120"
	e.fake.mu.Unlock()
	e.srv.TickAt(ctx, time.Now().Add(2*time.Hour))
	view := e.call("GET", "/v1/posts/"+id, e.key, nil, nil)
	if view.body["status"] != "scheduled" {
		t.Fatalf("a 429 on a scheduled post must not be final: %s", view.raw)
	}

	e.fake.mu.Lock()
	e.fake.liStatus, e.fake.liRetryAfter = 0, ""
	e.fake.mu.Unlock()
	e.srv.TickAt(ctx, time.Now().Add(3*time.Hour))
	if view := e.call("GET", "/v1/posts/"+id, e.key, nil, nil); view.body["status"] != "published" {
		t.Fatalf("retry: %s", view.raw)
	}
	if len(e.fake.linkedInPosts) != 1 {
		t.Fatalf("posts: %d", len(e.fake.linkedInPosts))
	}
}

func TestScheduledRetriesAreBounded(t *testing.T) {
	e := connectedEnv(t, "linkedin")
	ctx := context.Background()
	at := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "never", "scheduled_at": at}, postKey("bounded"))
	id := r.body["id"].(string)
	e.fake.mu.Lock()
	e.fake.liStatus = 503
	e.fake.mu.Unlock()
	for i := 1; i <= maxScheduledAttempts; i++ {
		e.srv.TickAt(ctx, time.Now().Add(time.Duration(i)*24*time.Hour))
	}
	view := e.call("GET", "/v1/posts/"+id, e.key, nil, nil)
	if view.body["status"] != "failed" || view.body["attempts"].(float64) != maxScheduledAttempts {
		t.Fatalf("after %d attempts: %s", maxScheduledAttempts, view.raw)
	}
}

func TestPermanentFailureOnAScheduledPostIsFinal(t *testing.T) {
	e := connectedEnv(t, "linkedin")
	at := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": "bad", "scheduled_at": at}, postKey("perm"))
	e.fake.mu.Lock()
	e.fake.liStatus = 422
	e.fake.mu.Unlock()
	e.srv.TickAt(context.Background(), time.Now().Add(2*time.Hour))
	if view := e.call("GET", "/v1/posts/"+r.body["id"].(string), e.key, nil, nil); view.body["status"] != "failed" {
		t.Fatalf("%s", view.raw)
	}
}

func TestLinkedInLengthIsCheckedBeforeSending(t *testing.T) {
	e := connectedEnv(t, "linkedin")
	max := e.srv.LinkedIn.Capabilities().MaxLength
	if max == 0 {
		t.Skip("no LinkedIn limit declared")
	}
	r := e.call("POST", "/v1/posts", e.key, map[string]string{"channel": "linkedin", "text": strings.Repeat("a", max+1)}, postKey("long"))
	if r.status != 422 || len(e.fake.linkedInPosts) != 0 {
		t.Fatalf("got %d %s", r.status, r.raw)
	}
}

func TestScheduledRetryDelayBacksOffAndHonorsTheProvider(t *testing.T) {
	if d := scheduledRetryDelay(1, 0); d != time.Minute+time.Second {
		t.Fatalf("first: %v", d)
	}
	if d := scheduledRetryDelay(3, 0); d != 4*time.Minute+time.Second {
		t.Fatalf("third: %v", d)
	}
	if d := scheduledRetryDelay(1, 15*time.Minute); d != 15*time.Minute+time.Second {
		t.Fatalf("Retry-After: %v", d)
	}
	if d := scheduledRetryDelay(2, 48*time.Hour); d != 6*time.Hour+time.Second {
		t.Fatalf("cap: %v", d)
	}
}

func orgOf(t *testing.T, e *env) string {
	t.Helper()
	return e.call("GET", "/v1/me", e.key, nil, nil).body["organization_id"].(string)
}
