package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// LinkedIn publishes text posts to a member's personal profile.
// Scopes: openid profile w_member_social (self-serve "Share on LinkedIn").
type LinkedIn struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthBase     string // https://www.linkedin.com
	APIBase      string // https://api.linkedin.com
	HTTP         *http.Client
}

const linkedInMaxLength = 3000

func (l *LinkedIn) Configured() bool { return l.ClientID != "" && l.ClientSecret != "" }

func (l *LinkedIn) Capabilities() Capabilities {
	return Capabilities{Publish: true, Schedule: true, Analytics: false, Formats: []string{"post"}, MaxLength: linkedInMaxLength}
}

func (l *LinkedIn) authBase() string {
	return strings.TrimRight(firstNonEmpty(l.AuthBase, "https://www.linkedin.com"), "/")
}
func (l *LinkedIn) apiBase() string {
	return strings.TrimRight(firstNonEmpty(l.APIBase, "https://api.linkedin.com"), "/")
}

// AuthURL is the consent page; state binds the callback to a connect session.
func (l *LinkedIn) AuthURL(state string) string {
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {l.ClientID},
		"redirect_uri":  {l.RedirectURL},
		"state":         {state},
		"scope":         {"openid profile w_member_social"},
	}
	return l.authBase() + "/oauth/v2/authorization?" + q.Encode()
}

// Exchange trades the authorization code for an access token.
func (l *LinkedIn) Exchange(ctx context.Context, code string) (token string, expiresAt time.Time, err error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {l.RedirectURL},
		"client_id":     {l.ClientID},
		"client_secret": {l.ClientSecret},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, l.authBase()+"/oauth/v2/accessToken", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, _, err := do(l.HTTP, req, "LinkedIn")
	if err != nil {
		return "", time.Time{}, err
	}
	var r struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if json.Unmarshal(body, &r) != nil || r.AccessToken == "" {
		return "", time.Time{}, Errf(CodeAuthFailed, "LinkedIn returned no access token")
	}
	if r.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	return r.AccessToken, expiresAt, nil
}

// Me reads the member's OpenID profile.
func (l *LinkedIn) Me(ctx context.Context, token string) (Profile, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, l.apiBase()+"/v2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	body, _, err := do(l.HTTP, req, "LinkedIn")
	if err != nil {
		return Profile{}, err
	}
	var p struct {
		Sub       string `json:"sub"`
		Name      string `json:"name"`
		GivenName string `json:"given_name"`
	}
	if json.Unmarshal(body, &p) != nil || p.Sub == "" {
		return Profile{}, Errf(CodeAuthFailed, "LinkedIn profile response had no member id")
	}
	return Profile{ID: p.Sub, Handle: firstNonEmpty(p.Name, p.GivenName, p.Sub), Name: p.Name}, nil
}

// Post publishes text and returns the post URN and its public URL. LinkedIn
// confirms with 201 and the URN in x-restli-id.
func (l *LinkedIn) Post(ctx context.Context, token, memberID, text string) (urn, publicURL string, err error) {
	if utf8.RuneCountInString(text) > linkedInMaxLength {
		return "", "", Errf(CodeUnsupportedFormat, "LinkedIn posts are limited to %d characters", linkedInMaxLength)
	}
	payload := map[string]any{
		"author":     "urn:li:person:" + memberID,
		"commentary": EncodeLinkedInCommentary(text),
		"visibility": "PUBLIC",
		"distribution": map[string]any{
			"feedDistribution":               "MAIN_FEED",
			"targetEntities":                 []any{},
			"thirdPartyDistributionChannels": []any{},
		},
		"lifecycleState":            "PUBLISHED",
		"isReshareDisabledByAuthor": false,
	}
	raw, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, l.apiBase()+"/rest/posts", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")
	req.Header.Set("Linkedin-Version", linkedInVersion())
	_, h, err := do(l.HTTP, req, "LinkedIn")
	if err != nil {
		return "", "", err
	}
	urn = strings.TrimSpace(h.Get("x-restli-id"))
	if urn == "" {
		// Accepted (2xx) without an id: published, but no URL can be built.
		return "", "", nil
	}
	if !linkedInPostID.MatchString(urn) {
		return "", "", Errf(CodeProviderUnavailable, "LinkedIn returned an unrecognised post id")
	}
	return urn, "https://www.linkedin.com/feed/update/" + urn + "/", nil
}

// Revoke invalidates the token at LinkedIn (best effort).
func (l *LinkedIn) Revoke(ctx context.Context, token string) error {
	form := url.Values{"token": {token}, "client_id": {l.ClientID}, "client_secret": {l.ClientSecret}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, l.authBase()+"/oauth/v2/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, _, err := do(l.HTTP, req, "LinkedIn")
	return err
}

var linkedInPostID = regexp.MustCompile(`^(urn:li:(share|ugcPost):[A-Za-z0-9_-]+|[0-9]+)$`)

// linkedInVersion defaults to the previous month: LinkedIn's monthly
// versions are sometimes not live on the 1st (upstream behaviour).
func linkedInVersion() string {
	if v := os.Getenv("LINKEDIN_API_VERSION"); v != "" {
		return v
	}
	return time.Now().UTC().AddDate(0, -1, 0).Format("200601")
}

// LinkedIn's "little text format" reserves these characters.
var linkedInEscaper = strings.NewReplacer(
	`\`, `\\`, `|`, `\|`, `{`, `\{`, `}`, `\}`, `@`, `\@`, `[`, `\[`, `]`, `\]`,
	`(`, `\(`, `)`, `\)`, `<`, `\<`, `>`, `\>`, `#`, `\#`, `*`, `\*`, `_`, `\_`, `~`, `\~`,
)

var linkedInToken = regexp.MustCompile(`(?i)https?://\S+|www\.\S+|#[\p{L}\p{M}\p{N}]+`)

// EncodeLinkedInCommentary escapes reserved characters and turns standalone
// #tags into native hashtags; URLs stay plain text.
func EncodeLinkedInCommentary(content string) string {
	var b strings.Builder
	prev := 0
	for _, m := range linkedInToken.FindAllStringIndex(content, -1) {
		start, end := m[0], m[1]
		b.WriteString(linkedInEscaper.Replace(content[prev:start]))
		tok := content[start:end]
		if isHashtag(content, start, tok) {
			b.WriteString(`{hashtag|\#|` + tok[1:] + `}`)
		} else {
			b.WriteString(linkedInEscaper.Replace(tok))
		}
		prev = end
	}
	b.WriteString(linkedInEscaper.Replace(content[prev:]))
	return b.String()
}

func isHashtag(content string, start int, tok string) bool {
	if !strings.HasPrefix(tok, "#") {
		return false
	}
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(content[:start])
		if unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune("/=&#\\", r) {
			return false
		}
	}
	for _, r := range tok[1:] {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
