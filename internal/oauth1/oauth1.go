// Package oauth1 signs requests with OAuth 1.0a HMAC-SHA1 (RFC 5849), the
// scheme X uses for user-context access. It replaces the dghubble/oauth1
// dependency used upstream with the ~80 lines this service actually needs.
package oauth1

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Credentials are the consumer (app) and, once issued, token (user) secrets.
type Credentials struct {
	ConsumerKey    string
	ConsumerSecret string
	Token          string
	TokenSecret    string
}

// Encode percent-encodes per RFC 3986 as OAuth requires (only A-Z a-z 0-9 - . _ ~ unescaped).
func Encode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteString("%")
			b.WriteString(strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}

// Signature computes the HMAC-SHA1 signature for method + baseURL (no query)
// over params (oauth_* plus query and form parameters, never a JSON body).
func Signature(method, baseURL string, params url.Values, consumerSecret, tokenSecret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(params))
	for _, k := range keys {
		vals := append([]string(nil), params[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			pairs = append(pairs, Encode(k)+"="+Encode(v))
		}
	}
	base := strings.ToUpper(method) + "&" + Encode(baseURL) + "&" + Encode(strings.Join(pairs, "&"))
	mac := hmac.New(sha1.New, []byte(Encode(consumerSecret)+"&"+Encode(tokenSecret)))
	mac.Write([]byte(base))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// Header builds the Authorization header value. extraOAuth carries protocol
// parameters such as oauth_callback or oauth_verifier; signedParams are query
// or form parameters that are part of the signature but not the header.
func Header(method, rawURL string, c Credentials, extraOAuth map[string]string, signedParams url.Values, nonce string, ts time.Time) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	all := url.Values{}
	for k, vs := range u.Query() {
		all[k] = append(all[k], vs...)
	}
	for k, vs := range signedParams {
		all[k] = append(all[k], vs...)
	}
	oauth := map[string]string{
		"oauth_consumer_key":     c.ConsumerKey,
		"oauth_nonce":            nonce,
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        strconv.FormatInt(ts.Unix(), 10),
		"oauth_version":          "1.0",
	}
	if c.Token != "" {
		oauth["oauth_token"] = c.Token
	}
	for k, v := range extraOAuth {
		oauth[k] = v
	}
	for k, v := range oauth {
		all.Set(k, v)
	}
	u.RawQuery = ""
	u.Fragment = ""
	oauth["oauth_signature"] = Signature(method, u.String(), all, c.ConsumerSecret, c.TokenSecret)

	keys := make([]string, 0, len(oauth))
	for k := range oauth {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, Encode(k)+`="`+Encode(oauth[k])+`"`)
	}
	return "OAuth " + strings.Join(parts, ", "), nil
}

// Nonce returns a random alphanumeric-safe nonce.
func Nonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
