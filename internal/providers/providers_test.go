package providers

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitThread(t *testing.T) {
	got := SplitThread("1/ one\n\n2/ two\n  \n3/ three")
	if !reflect.DeepEqual(got, []string{"1/ one", "2/ two", "3/ three"}) {
		t.Fatalf("thread = %q", got)
	}
	if got := SplitThread("A single post.\n\nWith two paragraphs."); len(got) != 1 {
		t.Fatalf("a non-numbered post must stay one post, got %q", got)
	}
}

func TestXLengthCountsURLsAs23(t *testing.T) {
	s := "see " + "https://example.com/" + strings.Repeat("a", 100)
	if n := XLength(s); n != 4+23 {
		t.Fatalf("XLength = %d", n)
	}
}

func TestLinkedInCommentaryEscaping(t *testing.T) {
	cases := map[string]string{
		"Hello (world) #launch":        `Hello \(world\) {hashtag|\#|launch}`,
		"email me@x.com":               `email me\@x.com`,
		"see https://a.io/x_(y) now":   `see https://a.io/x\_\(y\) now`,
		"C# is not a tag, nor is #123": `C\# is not a tag, nor is \#123`,
	}
	for in, want := range cases {
		if got := EncodeLinkedInCommentary(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
