package bashdiff

import (
	"strings"
	"testing"
)

func TestSplitWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", "a b c", []string{"a", "b", "c"}},
		{"collapses-runs", "a   b", []string{"a", "b"}},
		{"keeps-quoted-spaces", `'a b c' d`, []string{"'a b c'", "d"}},
		{"keeps-double-quoted-spaces", `"a b" c`, []string{`"a b"`, "c"}},
		{"empty", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := splitWords(tc.in)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("splitWords(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestJoinWords(t *testing.T) {
	t.Parallel()

	if got := joinWords([]string{"a", "b c"}); got != "a b c" {
		t.Errorf("joinWords = %q, want %q", got, "a b c")
	}
}

func TestHasGlobMeta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"plain", "plain", false},
		{"star", "a*b", true},
		{"question", "a?b", true},
		{"bracket", "[abc]", true},
		{"embedded-bracket", "/et[c]/", true},
		{"empty-bracket", "[]", false},
		{"unclosed-bracket", "[abc", false},
		{"trailing-bracket", "foo[]", false},
		{"literal-parens", "{{(#)}}", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasGlobMeta(tc.in); got != tc.want {
				t.Errorf("hasGlobMeta(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
