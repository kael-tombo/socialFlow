// Package providers talks to LinkedIn and X. Endpoint shapes, headers and
// payloads follow OpenPost's verified adapters (apps/server/internal/platform
// in kael-tombo/socialFlow, AGPL-3.0), reduced to text publishing.
package providers

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Error codes are the contract's error vocabulary (Banyan contract.ts errorResponse).
const (
	CodeAuthFailed          = "auth_failed"
	CodeRevoked             = "revoked"
	CodeRateLimited         = "rate_limited"
	CodeUnsupportedFormat   = "unsupported_format"
	CodeValidation          = "validation"
	CodeNotFound            = "not_found"
	CodeProviderUnavailable = "provider_unavailable"
)

// Error is a classified provider or request failure.
type Error struct {
	Code       string
	Message    string
	HTTPStatus int           // provider status, 0 when no response
	RetryAfter time.Duration // set for rate limits when the provider says
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Retryable reports whether trying again later can succeed without a human.
func (e *Error) Retryable() bool {
	return e.Code == CodeRateLimited || e.Code == CodeProviderUnavailable
}

func Errf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Profile is the connected identity.
type Profile struct {
	ID     string
	Handle string
	Name   string
}

// Capabilities mirror the contract's capabilitiesSchema.
type Capabilities struct {
	Publish   bool     `json:"publish"`
	Schedule  bool     `json:"schedule"`
	Analytics bool     `json:"analytics"`
	Formats   []string `json:"formats"`
	MaxLength int      `json:"max_length,omitempty"`
}

const maxProviderBody = 1 << 20

// do executes req and returns the body; any non-2xx becomes a classified *Error.
// Provider bodies are never echoed to callers beyond a short, trimmed reason.
func do(client *http.Client, req *http.Request, platform string) ([]byte, http.Header, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, &Error{Code: CodeProviderUnavailable, Message: platform + " could not be reached"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderBody))
	if err != nil {
		return nil, nil, &Error{Code: CodeProviderUnavailable, Message: "reading the " + platform + " response failed"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, classify(resp.StatusCode, resp.Header, body, platform)
	}
	return body, resp.Header, nil
}

func classify(status int, h http.Header, body []byte, platform string) *Error {
	reason := shortReason(body)
	e := &Error{HTTPStatus: status}
	lower := strings.ToLower(string(body))
	switch {
	case status == http.StatusUnauthorized:
		e.Code, e.Message = CodeAuthFailed, platform+" rejected the account's access token; reconnect the account"
	case status == http.StatusTooManyRequests:
		e.Code, e.Message = CodeRateLimited, platform+" rate limit reached"
		e.RetryAfter = retryAfter(h)
	case status == http.StatusPaymentRequired:
		e.Code, e.Message = CodeValidation, platform+" API credits are depleted for this app"
	case status == http.StatusForbidden && strings.Contains(lower, "duplicate"):
		e.Code, e.Message = CodeValidation, platform+" rejected the post as a duplicate of a recent one"
	case status == http.StatusForbidden:
		e.Code, e.Message = CodeAuthFailed, platform+" refused the action for this account"
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		e.Code, e.Message = CodeValidation, platform+" rejected the post"
	case status == http.StatusNotFound:
		e.Code, e.Message = CodeNotFound, platform+" could not find the resource"
	case status >= 500:
		e.Code, e.Message = CodeProviderUnavailable, platform+" is unavailable"
	default:
		e.Code, e.Message = CodeValidation, fmt.Sprintf("%s responded %d", platform, status)
	}
	if reason != "" {
		e.Message += " (" + reason + ")"
	}
	return e
}

var detailFields = regexp.MustCompile(`"(?:detail|message|error_description|title)"\s*:\s*"([^"]{1,200})"`)

func shortReason(body []byte) string {
	if m := detailFields.FindSubmatch(body); m != nil {
		return string(m[1])
	}
	return ""
}

func retryAfter(h http.Header) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s >= 0 {
			return time.Duration(s) * time.Second
		}
	}
	// X reports the reset as a unix timestamp.
	if v := h.Get("x-rate-limit-reset"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			if d := time.Until(time.Unix(ts, 0)); d > 0 {
				return d
			}
		}
	}
	return 0
}

var (
	paragraphBreak = regexp.MustCompile(`\n\s*\n`)
	threadNumber   = regexp.MustCompile(`^\s*1\s*/`)
	urlPattern     = regexp.MustCompile(`https?://\S+`)
)

// SplitThread turns Banyan's thread format (blank-line separated posts, the
// first starting "1/") into posts. Anything else is a single post.
func SplitThread(text string) []string {
	text = strings.TrimSpace(text)
	parts := paragraphBreak.Split(text, -1)
	if len(parts) > 1 && threadNumber.MatchString(parts[0]) {
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return []string{text}
}

// XLength approximates X's weighted length: every URL counts as 23.
func XLength(s string) int {
	n := utf8.RuneCountInString(urlPattern.ReplaceAllString(s, ""))
	return n + 23*len(urlPattern.FindAllString(s, -1))
}
