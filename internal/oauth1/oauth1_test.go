package oauth1

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// The worked example from X's "Creating a signature" documentation.
func TestSignatureMatchesXDocumentationVector(t *testing.T) {
	params := url.Values{
		"status":                 {"Hello Ladies + Gentlemen, a signed OAuth request!"},
		"include_entities":       {"true"},
		"oauth_consumer_key":     {"xvz1evFS4wEEPTGEFPHBog"},
		"oauth_nonce":            {"kYjzVBB8Y0ZFabxSWbWovY3uYSQ2pTgmZeNu2VS4cg"},
		"oauth_signature_method": {"HMAC-SHA1"},
		"oauth_timestamp":        {"1318622958"},
		"oauth_token":            {"370773112-GmHxMAgYyLbNEtIKZeRNFsMKPR9EyMZeS9weJAEb"},
		"oauth_version":          {"1.0"},
	}
	got := Signature("POST", "https://api.twitter.com/1.1/statuses/update.json", params,
		"kAcSOqF21Fu85e7zjz7ZN2U4ZRhfV3WpwPAoE3Z7kBw", "LswwdoUaIvS8ltyTt5jkRh4J50vUPVVHtR2YPi5kE")
	if want := "hCtSmYh+iHYCEqBWrE7C7hYmtUk="; got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestHeaderSignsQueryAndIncludesOAuthParamsOnly(t *testing.T) {
	c := Credentials{ConsumerKey: "ck", ConsumerSecret: "cs", Token: "tk", TokenSecret: "ts"}
	h, err := Header("GET", "https://api.twitter.com/2/users/me?user.fields=id,username", c, nil, nil, "n1", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "OAuth ") || !strings.Contains(h, `oauth_token="tk"`) || !strings.Contains(h, "oauth_signature=") {
		t.Fatalf("unexpected header %q", h)
	}
	if strings.Contains(h, "user.fields") {
		t.Fatalf("query parameters must not be placed in the header: %q", h)
	}
}

func TestEncode(t *testing.T) {
	if got := Encode("Ladies + Gentlemen!*~"); got != "Ladies%20%2B%20Gentlemen%21%2A~" {
		t.Fatalf("Encode = %q", got)
	}
}
