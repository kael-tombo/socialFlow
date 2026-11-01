package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kael-tombo/socialflow-lite/internal/providers"
)

func cfg(t *testing.T, li, x http.HandlerFunc) Config {
	t.Helper()
	liSrv := httptest.NewServer(li)
	xSrv := httptest.NewServer(x)
	t.Cleanup(liSrv.Close)
	t.Cleanup(xSrv.Close)
	return Config{
		PublicURL: "https://social.example.com", EncryptionOK: true, DatabaseOK: true, HTTP: liSrv.Client(),
		LinkedIn: &providers.LinkedIn{ClientID: "id", ClientSecret: "sec", RedirectURL: "https://social.example.com/oauth/linkedin/callback", AuthBase: liSrv.URL, HTTP: liSrv.Client()},
		X:        &providers.X{ConsumerKey: "ck", ConsumerSecret: "cs", CallbackURL: "https://social.example.com/oauth/x/callback", APIBase: xSrv.URL, HTTP: xSrv.Client()},
	}
}

func find(rs []Result, name string) Result {
	for _, r := range rs {
		if r.Name == name {
			return r
		}
	}
	return Result{}
}

func TestHealthyConfig(t *testing.T) {
	c := cfg(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"invalid_request","error_description":"Unable to retrieve access token: authorization code not found"}`))
		},
		func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte("oauth_token=t&oauth_token_secret=s&oauth_callback_confirmed=true"))
		})
	rs := Run(context.Background(), c)
	if Failed(rs) {
		t.Fatalf("unexpected failure: %+v", rs)
	}
	if find(rs, "LinkedIn credentials").Status != Pass || find(rs, "X credentials").Status != Pass {
		t.Fatalf("both providers should pass: %+v", rs)
	}
	if !strings.Contains(find(rs, "LinkedIn console").Detail, "https://social.example.com/oauth/linkedin/callback") {
		t.Fatal("the LinkedIn redirect URL to paste must be printed")
	}
	if !strings.Contains(find(rs, "X console").Detail, "https://social.example.com/oauth/x/callback") {
		t.Fatal("the X callback URL to paste must be printed")
	}
}

func TestWrongCredentials(t *testing.T) {
	c := cfg(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"invalid_client","error_description":"Client authentication failed"}`))
		},
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
			w.Write([]byte(`{"errors":[{"code":32,"message":"Could not authenticate you."}]}`))
		})
	rs := Run(context.Background(), c)
	li, x := find(rs, "LinkedIn credentials"), find(rs, "X credentials")
	if li.Status != Fail || !strings.Contains(li.Detail, "invalid_client") {
		t.Fatalf("LinkedIn: %+v", li)
	}
	if x.Status != Fail || !strings.Contains(x.Detail, "callback URL not registered exactly as https://social.example.com/oauth/x/callback") {
		t.Fatalf("X: %+v", x)
	}
	if !Failed(rs) {
		t.Fatal("Failed must report the failure")
	}
}

func TestXCallbackNotConfirmed(t *testing.T) {
	c := cfg(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(400) },
		func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("oauth_token=t&oauth_token_secret=s")) })
	if x := find(Run(context.Background(), c), "X credentials"); x.Status != Fail {
		t.Fatalf("an unconfirmed callback must fail: %+v", x)
	}
}

func TestUnconfiguredAndLocal(t *testing.T) {
	c := cfg(t, nil, nil)
	c.PublicURL = "http://localhost:8080"
	c.LinkedIn.ClientID, c.X.ConsumerKey = "", ""
	c.EncryptionOK, c.EncryptionErr = false, "bad key"
	rs := Run(context.Background(), c)
	if find(rs, "PUBLIC_URL").Status != Warn || find(rs, "ENCRYPTION_KEY").Status != Fail {
		t.Fatalf("%+v", rs)
	}
	if find(rs, "LinkedIn credentials").Status != Warn || find(rs, "X credentials").Status != Warn {
		t.Fatalf("missing credentials are a warning, not a failure: %+v", rs)
	}
	c.PublicURL = "http://social.example.com"
	if find(Run(context.Background(), c), "PUBLIC_URL").Status != Fail {
		t.Fatal("plain http on a public host must fail")
	}
}
