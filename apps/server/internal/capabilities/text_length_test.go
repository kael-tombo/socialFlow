package capabilities

import (
	"strings"
	"testing"

	"github.com/openpost/backend/internal/models"
	"github.com/stretchr/testify/require"
)

func TestTextLengthUsesXWeightedCounting(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "ASCII", text: "Hello, world!", want: 13},
		{name: "emoji", text: "Hello, world! 👋", want: 16},
		{name: "emoji sequence", text: "👨‍👩‍👧‍👦", want: 2},
		{name: "flag", text: "🇵🇹", want: 2},
		{name: "CJK", text: "日本語", want: 6},
		{name: "URL", text: "See https://example.com/this/is/a/long/path", want: 27},
		{name: "URL with balanced parentheses", text: "See https://en.wikipedia.org/wiki/Foo_(bar)", want: 27},
		{name: "URL with balanced parentheses before a period", text: "See https://en.wikipedia.org/wiki/Foo_(bar).", want: 28},
		{name: "URL inside parentheses", text: "(https://example.com/path)", want: 25},
		{name: "NFC normalization", text: "cafe\u0301", want: 4},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, TextLength(ProviderX, test.text))
		})
	}
}

func TestTextLengthUsesGraphemesForBluesky(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "ASCII", text: "Hello, world!", want: 13},
		{name: "CJK", text: "日本語", want: 3},
		{name: "combining mark", text: "café", want: 4},
		{name: "skin tone", text: "👍🏽", want: 1},
		{name: "flag", text: "🇵🇹", want: 1},
		{name: "ZWJ sequence", text: "👨‍👩‍👧‍👦", want: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, TextLength(ProviderBluesky, test.text))
		})
	}

	// 151 skin-toned thumbs are 151 graphemes and 302 code points: within
	// Bluesky's 300-grapheme limit, so the body must validate.
	body := strings.Repeat("👍🏽", 151)
	issues := Validate(ProviderBluesky, models.ContentProfileShortText, body, "", "", nil, nil)
	for _, issue := range issues {
		require.NotEqual(t, "text_too_long", issue.Code, issue.Message)
	}
	issues = Validate(ProviderBluesky, models.ContentProfileShortText, strings.Repeat("👍🏽", 301), "", "", nil, nil)
	require.NotEmpty(t, issues)
	require.Equal(t, "text_too_long", issues[0].Code)
}

func TestTextLengthCountsMastodonLinksAndRemoteMentionsLikeMastodon(t *testing.T) {
	longURL := "https://example.com/" + strings.Repeat("a", 80)
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "ASCII", text: "Hello, world!", want: 13},
		{name: "URL", text: "Read " + longURL, want: 28},
		{name: "URL before punctuation", text: "See " + longURL + ".", want: 28},
		{name: "no scheme", text: "www.example.com", want: 15},
		{name: "balanced parentheses", text: "https://en.wikipedia.org/wiki/Foo_(bar)", want: 23},
		{name: "URL in parentheses", text: "(https://example.com/path)", want: 25},
		{name: "host without a domain", text: "https://intranet/page", want: 21},
		{name: "remote mention", text: "@alice@example.social hi", want: 9},
		{name: "local mention", text: "@alice hi", want: 9},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, TextLength(ProviderMastodon, test.text))
		})
	}

	// 450 characters and a 100-character link are 474 characters for
	// Mastodon, which counts every link as 23: within its 500 limit.
	body := strings.Repeat("a", 450) + " " + longURL
	issues := Validate(ProviderMastodon, models.ContentProfileShortText, body, "", "", nil, nil)
	for _, issue := range issues {
		require.NotEqual(t, "text_too_long", issue.Code, issue.Message)
	}
	issues = Validate(ProviderMastodon, models.ContentProfileShortText, strings.Repeat("a", 477)+" "+longURL, "", "", nil, nil)
	require.NotEmpty(t, issues)
	require.Equal(t, "text_too_long", issues[0].Code)
}

func TestTextLengthBoundariesMatchProviderLimits(t *testing.T) {
	longURL := "https://example.com/" + strings.Repeat("a", 80)

	t.Run("X weighted URL boundary", func(t *testing.T) {
		atLimit := strings.Repeat("a", 256) + " " + longURL
		overLimit := strings.Repeat("a", 257) + " " + longURL
		require.Equal(t, 280, TextLength(ProviderX, atLimit))
		require.Equal(t, 281, TextLength(ProviderX, overLimit))
		standard, ok := ProviderTextLimit(ProviderX)
		require.True(t, ok, "X has a canonical text limit")
		require.Equal(t, 280, standard)
	})

	t.Run("Bluesky grapheme boundary", func(t *testing.T) {
		// Skin-toned thumbs are 1 grapheme but 2 code points each, so the
		// boundary must be measured in graphemes, not code points.
		atLimit := strings.Repeat("👍🏽", 300)
		overLimit := strings.Repeat("👍🏽", 301)
		require.Equal(t, 300, TextLength(ProviderBluesky, atLimit))
		require.Equal(t, 301, TextLength(ProviderBluesky, overLimit))
		issues := Validate(ProviderBluesky, models.ContentProfileShortText, atLimit, "", "", nil, nil)
		for _, issue := range issues {
			require.NotEqual(t, "text_too_long", issue.Code, issue.Message)
		}
		issues = Validate(ProviderBluesky, models.ContentProfileShortText, overLimit, "", "", nil, nil)
		require.NotEmpty(t, issues)
		require.Equal(t, "text_too_long", issues[0].Code)
	})

	t.Run("Mastodon remote mention boundary", func(t *testing.T) {
		// "@alice@example.social" counts as "@alice" (6 characters) even
		// though the raw mention is 20 characters.
		atLimit := strings.Repeat("a", 493) + " @alice@example.social"
		overLimit := strings.Repeat("a", 494) + " @alice@example.social"
		require.Equal(t, 500, TextLength(ProviderMastodon, atLimit))
		require.Equal(t, 501, TextLength(ProviderMastodon, overLimit))
		issues := Validate(ProviderMastodon, models.ContentProfileShortText, atLimit, "", "", nil, nil)
		for _, issue := range issues {
			require.NotEqual(t, "text_too_long", issue.Code, issue.Message)
		}
		issues = Validate(ProviderMastodon, models.ContentProfileShortText, overLimit, "", "", nil, nil)
		require.NotEmpty(t, issues)
		require.Equal(t, "text_too_long", issues[0].Code)
	})
}

func TestTextLengthKeepsOtherProvidersAtCodePoints(t *testing.T) {
	require.Equal(t, 3, TextLength(ProviderMastodon, "日本語"))
	require.Equal(t, 5, TextLength(ProviderMastodon, "cafe\u0301"))
}

func TestTextLengthUsesUTF8BytesForThreads(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "ASCII", text: "SocialFlow.ai", want: 8},
		{name: "accented letter", text: "é", want: 2},
		{name: "curly apostrophe", text: "’", want: 3},
		{name: "emoji", text: "👋", want: 4},
		{name: "ZWJ emoji", text: "👨‍👩‍👧‍👦", want: 25},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, TextLength(ProviderThreads, test.text))
		})
	}
}
