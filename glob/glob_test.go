package glob_test

import (
	"testing"

	"github.com/hoshsadiq/godotenv/glob"
)

func TestMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		s       string
		want    bool
	}{
		// Literals.
		{name: "literal match", pattern: "abc", s: "abc", want: true},
		{name: "literal mismatch", pattern: "abc", s: "abd", want: false},
		{name: "literal shorter string", pattern: "abc", s: "ab", want: false},
		{name: "literal longer string", pattern: "abc", s: "abcd", want: false},
		{name: "empty pattern empty string", pattern: "", s: "", want: true},
		{name: "empty pattern non-empty string", pattern: "", s: "a", want: false},
		{name: "non-empty pattern empty string", pattern: "a", s: "", want: false},

		// Star.
		{name: "star empty", pattern: "*", s: "", want: true},
		{name: "star any", pattern: "*", s: "abc", want: true},
		{name: "star prefix", pattern: "a*", s: "abc", want: true},
		{name: "star prefix exact", pattern: "a*", s: "a", want: true},
		{name: "star prefix mismatch", pattern: "a*", s: "b", want: false},
		{name: "star suffix", pattern: "*c", s: "abc", want: true},
		{name: "star suffix mismatch", pattern: "*c", s: "abd", want: false},
		{name: "star middle", pattern: "a*c", s: "abc", want: true},
		{name: "star middle empty", pattern: "a*c", s: "ac", want: true},
		{name: "star middle mismatch", pattern: "a*c", s: "abd", want: false},
		{name: "star both sides", pattern: "*a*", s: "bab", want: true},
		{name: "star both sides absent", pattern: "*a*", s: "bcd", want: false},
		{name: "multiple stars", pattern: "a*b*c", s: "axbyc", want: true},
		{name: "multiple stars mismatch", pattern: "a*b*c", s: "axbyd", want: false},
		{name: "star backtrack", pattern: "*ab", s: "aab", want: true},
		{name: "star backtrack multiple", pattern: "*a*b", s: "aXaYb", want: true},
		{name: "class backtrack", pattern: "*[ab]", s: "xac", want: false},
		{name: "escaped star backtrack", pattern: `*\*`, s: "a*", want: true},

		// Question mark, including multi-byte runes.
		{name: "question one byte", pattern: "?", s: "a", want: true},
		{name: "question empty", pattern: "?", s: "", want: false},
		{name: "question too long", pattern: "?", s: "ab", want: false},
		{name: "question middle", pattern: "a?c", s: "abc", want: true},
		{name: "question middle missing", pattern: "a?c", s: "ac", want: false},
		{name: "question multibyte", pattern: "?", s: "é", want: true},
		{name: "question multibyte euro", pattern: "?", s: "€", want: true},
		{name: "question multibyte middle", pattern: "a?c", s: "aéc", want: true},
		{name: "question multibyte counts one rune", pattern: "??", s: "é", want: false},
		{name: "question multibyte two runes", pattern: "??", s: "éé", want: true},

		// Character classes.
		{name: "class member", pattern: "[abc]", s: "a", want: true},
		{name: "class non-member", pattern: "[abc]", s: "d", want: false},
		{name: "class range", pattern: "[a-z]", s: "m", want: true},
		{name: "class range outside", pattern: "[a-z]", s: "M", want: false},
		{name: "class negated bang", pattern: "[!abc]", s: "d", want: true},
		{name: "class negated bang member", pattern: "[!abc]", s: "a", want: false},
		{name: "class negated caret", pattern: "[^abc]", s: "d", want: true},
		{name: "class negated caret member", pattern: "[^abc]", s: "a", want: false},
		{name: "class close bracket first", pattern: "[]a]", s: "]", want: true},
		{name: "class close bracket first member", pattern: "[]a]", s: "a", want: true},
		{name: "class close bracket first non-member", pattern: "[]a]", s: "b", want: false},
		{name: "class range with trailing dash", pattern: "[a-]", s: "a", want: true},
		{name: "class range with trailing dash literal", pattern: "[a-]", s: "-", want: true},
		{name: "class negated close bracket", pattern: "[!]]", s: "]", want: false},
		{name: "class negated close bracket other", pattern: "[!]]", s: "a", want: true},
		{name: "class unterminated literal", pattern: "[abc", s: "[abc", want: true},
		{name: "class unterminated lone bracket", pattern: "[", s: "[", want: true},
		{name: "class unterminated lone bracket other", pattern: "[", s: "a", want: false},
		{name: "class unterminated partial", pattern: "[abc", s: "[", want: false},

		// Escapes.
		{name: "escaped star literal", pattern: `\*`, s: "*", want: true},
		{name: "escaped star not wildcard", pattern: `\*`, s: "a", want: false},
		{name: "escaped question literal", pattern: `\?`, s: "?", want: true},
		{name: "escaped bracket literal", pattern: `\[`, s: "[", want: true},
		{name: "escaped backslash literal", pattern: `\\`, s: `\`, want: true},
		{name: "escaped star in middle", pattern: `a\*b`, s: "a*b", want: true},
		{name: "escaped star in middle mismatch", pattern: `a\*b`, s: "axb", want: false},
		{name: "escaped star mismatch no star", pattern: `\*a`, s: "a*a", want: false},
		{name: "trailing backslash literal", pattern: `a\`, s: `a\`, want: true},

		// No-match fast path: a literal run absent from s rejects without a scan.
		{name: "fast path literal absent", pattern: "abc", s: "xyz", want: false},
		{name: "fast path run absent", pattern: "a*b", s: "xyz", want: false},
		{name: "fast path one run absent", pattern: "a*b", s: "axc", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := glob.Match([]byte(tt.pattern), []byte(tt.s)); got != tt.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.s, got, tt.want)
			}
		})
	}
}

func TestMatchString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		s       string
		want    bool
	}{
		{name: "match", pattern: "a*c", s: "abc", want: true},
		{name: "no match", pattern: "a*c", s: "abd", want: false},
		{name: "empty", pattern: "", s: "", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := glob.MatchString(tt.pattern, tt.s); got != tt.want {
				t.Errorf("MatchString(%q, %q) = %v, want %v", tt.pattern, tt.s, got, tt.want)
			}
		})
	}
}

func TestMatchPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		s       string
		wantN   int
		wantOK  bool
	}{
		{name: "literal prefix", pattern: "abc", s: "abcdef", wantN: 3, wantOK: true},
		{name: "literal not prefix", pattern: "abc", s: "ab", wantN: 0, wantOK: false},
		{name: "literal mismatch", pattern: "abc", s: "xabc", wantN: 0, wantOK: false},
		{name: "empty pattern", pattern: "", s: "abc", wantN: 0, wantOK: true},
		{name: "empty both", pattern: "", s: "", wantN: 0, wantOK: true},
		{name: "star whole", pattern: "*", s: "abc", wantN: 3, wantOK: true},
		{name: "star empty", pattern: "*", s: "", wantN: 0, wantOK: true},
		{name: "star prefix", pattern: "a*", s: "abc", wantN: 3, wantOK: true},
		{name: "star prefix exact", pattern: "a*", s: "a", wantN: 1, wantOK: true},
		{name: "star prefix mismatch", pattern: "x*", s: "abc", wantN: 0, wantOK: false},
		{name: "star prefix not at start", pattern: "a*", s: "ba", wantN: 0, wantOK: false},
		{name: "star suffix", pattern: "*c", s: "abc", wantN: 3, wantOK: true},
		{name: "star suffix absent", pattern: "*x", s: "abc", wantN: 0, wantOK: false},
		{name: "star middle", pattern: "a*c", s: "abc", wantN: 3, wantOK: true},
		{name: "star middle longer", pattern: "a*c", s: "abcd", wantN: 3, wantOK: true},
		{name: "star middle empty", pattern: "a*c", s: "ac", wantN: 2, wantOK: true},
		{name: "star middle mismatch", pattern: "a*c", s: "abd", wantN: 0, wantOK: false},
		{name: "star middle prefix absent", pattern: "a*c", s: "xbc", wantN: 0, wantOK: false},
		{name: "star middle not at start", pattern: "a*c", s: "xac", wantN: 0, wantOK: false},
		{name: "star suffix absent longer", pattern: "*x", s: "abcx", wantN: 4, wantOK: true},
		{name: "star last occurrence", pattern: "a*b", s: "aXbYb", wantN: 5, wantOK: true},
		{name: "star last occurrence exact", pattern: "a*b", s: "ab", wantN: 2, wantOK: true},
		{name: "star last occurrence one", pattern: "a*b", s: "aXb", wantN: 3, wantOK: true},
		{name: "star last occurrence absent", pattern: "a*b", s: "aX", wantN: 0, wantOK: false},
		{name: "star both sides", pattern: "*a*", s: "ba", wantN: 2, wantOK: true},
		{name: "star both sides prefix", pattern: "*a*", s: "ab", wantN: 2, wantOK: true},
		{name: "star both sides absent", pattern: "*a*", s: "b", wantN: 0, wantOK: false},
		{name: "star both sides empty", pattern: "*a*", s: "", wantN: 0, wantOK: false},
		{name: "question", pattern: "?", s: "abc", wantN: 1, wantOK: true},
		{name: "question empty", pattern: "?", s: "", wantN: 0, wantOK: false},
		{name: "question multibyte", pattern: "?", s: "é", wantN: 2, wantOK: true},
		{name: "question multibyte longer", pattern: "?", s: "éx", wantN: 2, wantOK: true},
		{name: "question middle", pattern: "a?c", s: "abcd", wantN: 3, wantOK: true},
		{name: "question middle missing", pattern: "a?c", s: "ac", wantN: 0, wantOK: false},
		{name: "multiple stars", pattern: "a*b*c", s: "axbyc", wantN: 5, wantOK: true},
		{name: "multiple stars trailing", pattern: "a*b*c", s: "axbycd", wantN: 5, wantOK: true},
		{name: "class", pattern: "[a-z]*", s: "abc", wantN: 3, wantOK: true},
		{name: "class mismatch", pattern: "[a-z]*", s: "1bc", wantN: 0, wantOK: false},
		{name: "escaped star", pattern: `a\*b`, s: "a*bc", wantN: 3, wantOK: true},
		{name: "escaped star mismatch", pattern: `a\*b`, s: "axbc", wantN: 0, wantOK: false},
		{name: "fast path run absent", pattern: "a*b", s: "xyz", wantN: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n, ok := glob.MatchPrefix([]byte(tt.pattern), []byte(tt.s))
			if n != tt.wantN || ok != tt.wantOK {
				t.Errorf("MatchPrefix(%q, %q) = (%d, %v), want (%d, %v)",
					tt.pattern, tt.s, n, ok, tt.wantN, tt.wantOK)
			}
		})
	}
}

func TestMatchSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		s       string
		wantN   int
		wantOK  bool
	}{
		{name: "literal suffix", pattern: "abc", s: "xabc", wantN: 3, wantOK: true},
		{name: "literal not suffix", pattern: "abc", s: "abcx", wantN: 0, wantOK: false},
		{name: "empty pattern", pattern: "", s: "abc", wantN: 0, wantOK: true},
		{name: "empty both", pattern: "", s: "", wantN: 0, wantOK: true},
		{name: "star whole", pattern: "*", s: "abc", wantN: 3, wantOK: true},
		{name: "star empty", pattern: "*", s: "", wantN: 0, wantOK: true},
		{name: "star suffix", pattern: "*c", s: "abc", wantN: 3, wantOK: true},
		{name: "star suffix absent", pattern: "*x", s: "abc", wantN: 0, wantOK: false},
		{name: "star prefix", pattern: "a*", s: "xabc", wantN: 3, wantOK: true},
		{name: "star prefix exact", pattern: "a*", s: "abc", wantN: 3, wantOK: true},
		{name: "star prefix absent", pattern: "a*", s: "xbc", wantN: 0, wantOK: false},
		{name: "star middle", pattern: "a*c", s: "xabc", wantN: 3, wantOK: true},
		{name: "star middle absent", pattern: "a*c", s: "xabd", wantN: 0, wantOK: false},
		{name: "star both sides", pattern: "*b*", s: "abc", wantN: 3, wantOK: true},
		{name: "star both sides absent", pattern: "*b*", s: "ac", wantN: 0, wantOK: false},
		{name: "question", pattern: "?", s: "abc", wantN: 1, wantOK: true},
		{name: "question empty", pattern: "?", s: "", wantN: 0, wantOK: false},
		{name: "question multibyte", pattern: "?", s: "é", wantN: 2, wantOK: true},
		{name: "question middle", pattern: "a?c", s: "xabc", wantN: 3, wantOK: true},
		{name: "question middle absent", pattern: "a?c", s: "abcx", wantN: 0, wantOK: false},
		{name: "class", pattern: "[a-z]", s: "ab", wantN: 1, wantOK: true},
		{name: "class absent", pattern: "[a-z]", s: "a1", wantN: 0, wantOK: false},
		{name: "escaped star", pattern: `a\*b`, s: "xa*b", wantN: 3, wantOK: true},
		{name: "fast path run absent", pattern: "a*b", s: "xyz", wantN: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n, ok := glob.MatchSuffix([]byte(tt.pattern), []byte(tt.s))
			if n != tt.wantN || ok != tt.wantOK {
				t.Errorf("MatchSuffix(%q, %q) = (%d, %v), want (%d, %v)",
					tt.pattern, tt.s, n, ok, tt.wantN, tt.wantOK)
			}
		})
	}
}

func TestFind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pattern   string
		s         string
		wantStart int
		wantEnd   int
		wantOK    bool
	}{
		{name: "literal found", pattern: "abc", s: "xabcx", wantStart: 1, wantEnd: 4, wantOK: true},
		{name: "literal at start", pattern: "abc", s: "abcx", wantStart: 0, wantEnd: 3, wantOK: true},
		{name: "literal at end", pattern: "abc", s: "xabc", wantStart: 1, wantEnd: 4, wantOK: true},
		{name: "literal not found", pattern: "abc", s: "xyz", wantStart: 0, wantEnd: 0, wantOK: false},
		{name: "empty pattern", pattern: "", s: "abc", wantStart: 0, wantEnd: 0, wantOK: true},
		{name: "empty both", pattern: "", s: "", wantStart: 0, wantEnd: 0, wantOK: true},
		{name: "star whole", pattern: "*", s: "abc", wantStart: 0, wantEnd: 3, wantOK: true},
		{name: "star empty", pattern: "*", s: "", wantStart: 0, wantEnd: 0, wantOK: true},
		{name: "star suffix", pattern: "*c", s: "abc", wantStart: 0, wantEnd: 3, wantOK: true},
		{name: "star prefix", pattern: "a*", s: "xabc", wantStart: 1, wantEnd: 4, wantOK: true},
		{name: "star middle", pattern: "a*c", s: "xabcx", wantStart: 1, wantEnd: 4, wantOK: true},
		{name: "star middle absent", pattern: "a*c", s: "xyz", wantStart: 0, wantEnd: 0, wantOK: false},
		{name: "star both sides", pattern: "*b*", s: "abc", wantStart: 0, wantEnd: 3, wantOK: true},
		{name: "star both sides absent", pattern: "*b*", s: "ac", wantStart: 0, wantEnd: 0, wantOK: false},
		{name: "question", pattern: "?", s: "abc", wantStart: 0, wantEnd: 1, wantOK: true},
		{name: "question empty", pattern: "?", s: "", wantStart: 0, wantEnd: 0, wantOK: false},
		{name: "question multibyte", pattern: "?", s: "é", wantStart: 0, wantEnd: 2, wantOK: true},
		{name: "question multibyte longer", pattern: "?", s: "éx", wantStart: 0, wantEnd: 2, wantOK: true},
		{name: "question middle", pattern: "a?c", s: "xabcx", wantStart: 1, wantEnd: 4, wantOK: true},
		{name: "question middle absent", pattern: "a?c", s: "xyz", wantStart: 0, wantEnd: 0, wantOK: false},
		{name: "longest earliest", pattern: "a*b", s: "xaXbYb", wantStart: 1, wantEnd: 6, wantOK: true},
		{name: "star backtrack find", pattern: "a*b", s: "xaXb", wantStart: 1, wantEnd: 4, wantOK: true},
		{name: "longest exact", pattern: "a*b", s: "ab", wantStart: 0, wantEnd: 2, wantOK: true},
		{name: "longest absent", pattern: "a*b", s: "ba", wantStart: 0, wantEnd: 0, wantOK: false},
		{name: "class", pattern: "[a-z]", s: "1a2", wantStart: 1, wantEnd: 2, wantOK: true},
		{name: "class absent", pattern: "[a-z]", s: "123", wantStart: 0, wantEnd: 0, wantOK: false},
		{name: "escaped star", pattern: `a\*b`, s: "xa*bx", wantStart: 1, wantEnd: 4, wantOK: true},
		{name: "fast path run absent", pattern: "a*b", s: "xyz", wantStart: 0, wantEnd: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			start, end, ok := glob.Find([]byte(tt.pattern), []byte(tt.s))
			if start != tt.wantStart || end != tt.wantEnd || ok != tt.wantOK {
				t.Errorf("Find(%q, %q) = (%d, %d, %v), want (%d, %d, %v)",
					tt.pattern, tt.s, start, end, ok, tt.wantStart, tt.wantEnd, tt.wantOK)
			}
		})
	}
}
