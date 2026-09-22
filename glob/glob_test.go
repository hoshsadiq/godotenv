package glob_test

import (
	"bytes"
	"slices"
	"testing"
	"time"

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
		{name: "star question multibyte one rune", pattern: "*?", s: "€", want: true},
		{name: "star question multibyte needs two runes", pattern: "*??", s: "€", want: false},
		{name: "star question multibyte two runes", pattern: "*??", s: "€€", want: true},

		// '?' treats a byte that cannot start a rune as a character of its own,
		// as bash does, but must not split a valid rune.
		{name: "question lone continuation byte", pattern: "?", s: "\x80", want: true},
		{name: "question after literal lone continuation byte", pattern: "a?", s: "a\xa9", want: true},
		{name: "question truncated rune bytes", pattern: "??", s: "\xe2\x82", want: true},
		{name: "question does not split valid rune", pattern: "??", s: "€", want: false},

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

func TestMatchPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		s       string
		wantEnd int
		wantOK  bool
	}{
		{name: "literal prefix", pattern: "abc", s: "abcdef", wantEnd: 3, wantOK: true},
		{name: "literal not prefix", pattern: "abc", s: "ab", wantEnd: 0, wantOK: false},
		{name: "literal mismatch", pattern: "abc", s: "xabc", wantEnd: 0, wantOK: false},
		{name: "empty pattern", pattern: "", s: "abc", wantEnd: 0, wantOK: true},
		{name: "empty both", pattern: "", s: "", wantEnd: 0, wantOK: true},
		{name: "star whole", pattern: "*", s: "abc", wantEnd: 3, wantOK: true},
		{name: "star empty", pattern: "*", s: "", wantEnd: 0, wantOK: true},
		{name: "star prefix", pattern: "a*", s: "abc", wantEnd: 3, wantOK: true},
		{name: "star prefix exact", pattern: "a*", s: "a", wantEnd: 1, wantOK: true},
		{name: "star prefix mismatch", pattern: "x*", s: "abc", wantEnd: 0, wantOK: false},
		{name: "star prefix not at start", pattern: "a*", s: "ba", wantEnd: 0, wantOK: false},
		{name: "star suffix", pattern: "*c", s: "abc", wantEnd: 3, wantOK: true},
		{name: "star suffix absent", pattern: "*x", s: "abc", wantEnd: 0, wantOK: false},
		{name: "star middle", pattern: "a*c", s: "abc", wantEnd: 3, wantOK: true},
		{name: "star middle longer", pattern: "a*c", s: "abcd", wantEnd: 3, wantOK: true},
		{name: "star middle empty", pattern: "a*c", s: "ac", wantEnd: 2, wantOK: true},
		{name: "star middle mismatch", pattern: "a*c", s: "abd", wantEnd: 0, wantOK: false},
		{name: "star middle prefix absent", pattern: "a*c", s: "xbc", wantEnd: 0, wantOK: false},
		{name: "star middle not at start", pattern: "a*c", s: "xac", wantEnd: 0, wantOK: false},
		{name: "star suffix absent longer", pattern: "*x", s: "abcx", wantEnd: 4, wantOK: true},
		{name: "star last occurrence", pattern: "a*b", s: "aXbYb", wantEnd: 5, wantOK: true},
		{name: "star last occurrence exact", pattern: "a*b", s: "ab", wantEnd: 2, wantOK: true},
		{name: "star last occurrence one", pattern: "a*b", s: "aXb", wantEnd: 3, wantOK: true},
		{name: "star last occurrence absent", pattern: "a*b", s: "aX", wantEnd: 0, wantOK: false},
		{name: "star both sides", pattern: "*a*", s: "ba", wantEnd: 2, wantOK: true},
		{name: "star both sides prefix", pattern: "*a*", s: "ab", wantEnd: 2, wantOK: true},
		{name: "star both sides absent", pattern: "*a*", s: "b", wantEnd: 0, wantOK: false},
		{name: "star both sides empty", pattern: "*a*", s: "", wantEnd: 0, wantOK: false},
		{name: "question", pattern: "?", s: "abc", wantEnd: 1, wantOK: true},
		{name: "question empty", pattern: "?", s: "", wantEnd: 0, wantOK: false},
		{name: "question multibyte", pattern: "?", s: "é", wantEnd: 2, wantOK: true},
		{name: "question multibyte longer", pattern: "?", s: "éx", wantEnd: 2, wantOK: true},
		{name: "star question multibyte needs two runes", pattern: "*??", s: "€", wantEnd: 0, wantOK: false},
		{name: "question middle", pattern: "a?c", s: "abcd", wantEnd: 3, wantOK: true},
		{name: "question middle missing", pattern: "a?c", s: "ac", wantEnd: 0, wantOK: false},
		{name: "multiple stars", pattern: "a*b*c", s: "axbyc", wantEnd: 5, wantOK: true},
		{name: "multiple stars trailing", pattern: "a*b*c", s: "axbycd", wantEnd: 5, wantOK: true},
		{name: "class", pattern: "[a-z]*", s: "abc", wantEnd: 3, wantOK: true},
		{name: "class mismatch", pattern: "[a-z]*", s: "1bc", wantEnd: 0, wantOK: false},
		{name: "escaped star", pattern: `a\*b`, s: "a*bc", wantEnd: 3, wantOK: true},
		{name: "escaped star mismatch", pattern: `a\*b`, s: "axbc", wantEnd: 0, wantOK: false},
		{name: "fast path run absent", pattern: "a*b", s: "xyz", wantEnd: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			start, end, ok := glob.MatchPrefix([]byte(tt.pattern), []byte(tt.s))
			if start != 0 || end != tt.wantEnd || ok != tt.wantOK {
				t.Errorf("MatchPrefix(%q, %q) = (%d, %d, %v), want (0, %d, %v)",
					tt.pattern, tt.s, start, end, ok, tt.wantEnd, tt.wantOK)
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
		wantEnd int
		wantOK  bool
	}{
		{name: "literal suffix", pattern: "abc", s: "xabc", wantEnd: 3, wantOK: true},
		{name: "literal not suffix", pattern: "abc", s: "abcx", wantEnd: 0, wantOK: false},
		{name: "empty pattern", pattern: "", s: "abc", wantEnd: 0, wantOK: true},
		{name: "empty both", pattern: "", s: "", wantEnd: 0, wantOK: true},
		{name: "star whole", pattern: "*", s: "abc", wantEnd: 3, wantOK: true},
		{name: "star empty", pattern: "*", s: "", wantEnd: 0, wantOK: true},
		{name: "star suffix", pattern: "*c", s: "abc", wantEnd: 3, wantOK: true},
		{name: "star suffix absent", pattern: "*x", s: "abc", wantEnd: 0, wantOK: false},
		{name: "star prefix", pattern: "a*", s: "xabc", wantEnd: 3, wantOK: true},
		{name: "star prefix exact", pattern: "a*", s: "abc", wantEnd: 3, wantOK: true},
		{name: "star prefix absent", pattern: "a*", s: "xbc", wantEnd: 0, wantOK: false},
		{name: "star middle", pattern: "a*c", s: "xabc", wantEnd: 3, wantOK: true},
		{name: "star middle absent", pattern: "a*c", s: "xabd", wantEnd: 0, wantOK: false},
		{name: "star both sides", pattern: "*b*", s: "abc", wantEnd: 3, wantOK: true},
		{name: "star both sides absent", pattern: "*b*", s: "ac", wantEnd: 0, wantOK: false},
		{name: "question", pattern: "?", s: "abc", wantEnd: 1, wantOK: true},
		{name: "question empty", pattern: "?", s: "", wantEnd: 0, wantOK: false},
		{name: "question multibyte", pattern: "?", s: "é", wantEnd: 2, wantOK: true},
		{name: "question middle", pattern: "a?c", s: "xabc", wantEnd: 3, wantOK: true},
		{name: "star question multibyte needs two runes", pattern: "*??", s: "€", wantEnd: 0, wantOK: false},
		{name: "question middle absent", pattern: "a?c", s: "abcx", wantEnd: 0, wantOK: false},
		{name: "class", pattern: "[a-z]", s: "ab", wantEnd: 1, wantOK: true},
		{name: "class absent", pattern: "[a-z]", s: "a1", wantEnd: 0, wantOK: false},
		{name: "escaped star", pattern: `a\*b`, s: "xa*b", wantEnd: 3, wantOK: true},
		{name: "fast path run absent", pattern: "a*b", s: "xyz", wantEnd: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			start, end, ok := glob.MatchSuffix([]byte(tt.pattern), []byte(tt.s))
			wantStart, wantEnd := 0, 0
			if tt.wantOK {
				wantStart, wantEnd = len(tt.s)-tt.wantEnd, len(tt.s)
			}
			if start != wantStart || end != wantEnd || ok != tt.wantOK {
				t.Errorf("MatchSuffix(%q, %q) = (%d, %d, %v), want (%d, %d, %v)",
					tt.pattern, tt.s, start, end, ok, wantStart, wantEnd, tt.wantOK)
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
		{name: "star question multibyte needs two runes", pattern: "*??", s: "€", wantStart: 0, wantEnd: 0, wantOK: false},
		{name: "question middle", pattern: "a?c", s: "xabcx", wantStart: 1, wantEnd: 4, wantOK: true},
		{name: "question middle absent", pattern: "a?c", s: "xyz", wantStart: 0, wantEnd: 0, wantOK: false},
		{name: "longest earliest", pattern: "a*b", s: "xaXbYb", wantStart: 1, wantEnd: 6, wantOK: true},
		{name: "star backtrack find", pattern: "a*b", s: "xaXb", wantStart: 1, wantEnd: 4, wantOK: true},
		{name: "longest exact", pattern: "a*b", s: "ab", wantStart: 0, wantEnd: 2, wantOK: true},
		{name: "longest absent", pattern: "a*b", s: "ba", wantStart: 0, wantEnd: 0, wantOK: false},
		{name: "leftmost longest two stars", pattern: "*b*a", s: "abab", wantStart: 0, wantEnd: 3, wantOK: true},
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

// TestMatchLinear guards against a super-linear search. The matcher used to try
// every substring, so a pattern whose literals occur in the value but which
// cannot match took minutes on a large value (Find was O(n^3)).
func TestMatchLinear(t *testing.T) {
	t.Parallel()

	// "aaa...ab" contains both literals of "*b*a" but has no 'a' after the
	// 'b', so nothing matches and maybeMatch cannot prune the search.
	value := append(bytes.Repeat([]byte("a"), 10000), 'b')
	pattern := []byte("*b*a")

	calls := []struct {
		name string
		fn   func() bool
	}{
		{"Match", func() bool { return glob.Match(pattern, value) }},
		{"MatchPrefix", func() bool { _, _, ok := glob.MatchPrefix(pattern, value); return ok }},
		{"MatchSuffix", func() bool { _, _, ok := glob.MatchSuffix(pattern, value); return ok }},
		{"Find", func() bool { _, _, ok := glob.Find(pattern, value); return ok }},
	}

	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			done := make(chan bool, 1)
			go func() { done <- c.fn() }()

			select {
			case matched := <-done:
				if matched {
					t.Fatalf("%s(%q, value) matched, want no match", c.name, pattern)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("%s(%q, value) did not finish in 2s; the search is super-linear", c.name, pattern)
			}
		})
	}
}

// TestMatchManyBracketsLinear guards against quadratic class scanning. A pattern
// that is a long run of '[' has no closing ']', so each '[' used to rescan to
// the end of the pattern.
func TestMatchManyBracketsLinear(t *testing.T) {
	t.Parallel()

	pattern := bytes.Repeat([]byte("["), 200000)
	s := []byte("x")

	done := make(chan bool, 1)
	go func() { done <- glob.Match(pattern, s) }()

	select {
	case matched := <-done:
		if matched {
			t.Fatal("Match with a long run of '[' matched, want no match")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Match with a long run of '[' did not finish in 2s; class scanning is quadratic")
	}
}

func TestCompile(t *testing.T) {
	t.Parallel()

	p := glob.Compile([]byte("a*b"))
	if !p.Match([]byte("aXb")) {
		t.Error("Compile(a*b).Match(aXb) = false, want true")
	}
	if _, end, ok := p.MatchPrefix([]byte("aXbYb")); !ok || end != 5 {
		t.Errorf("Compile(a*b).MatchPrefix(aXbYb) = (_, %d, %v), want (_, 5, true)", end, ok)
	}
	if _, end, ok := p.MatchSuffix([]byte("xaXb")); !ok || end != 4 {
		t.Errorf("Compile(a*b).MatchSuffix(xaXb) = (_, %d, %v), want (_, 4, true)", end, ok)
	}
	if start, end, ok := p.Find([]byte("xaXb")); !ok || start != 1 || end != 4 {
		t.Errorf("Compile(a*b).Find(xaXb) = (%d, %d, %v), want (1, 4, true)", start, end, ok)
	}
}

func TestPrefixMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		s       string
		want    []int
	}{
		{name: "literal", pattern: "a", s: "abc", want: []int{1}},
		{name: "literal absent", pattern: "x", s: "abc", want: nil},
		{name: "empty pattern", pattern: "", s: "abc", want: []int{0}},
		{name: "star every prefix", pattern: "*", s: "abc", want: []int{0, 1, 2, 3}},
		{name: "star middle", pattern: "a*b", s: "aXbYb", want: []int{3, 5}},
		{name: "question rune", pattern: "?", s: "éx", want: []int{2}},
		{name: "question mid rune", pattern: "\xc3?", s: "é", want: nil},
		{name: "question lone continuation byte", pattern: "a?", s: "a\x80", want: []int{2}},
		{name: "question does not split valid rune", pattern: "??", s: "€", want: nil},
		{name: "class", pattern: "[ab]", s: "abc", want: []int{1}},
		{name: "run absent", pattern: "a*b", s: "xyz", want: nil},
		{name: "mid rune byte", pattern: "\xc3", s: "é", want: []int{1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := glob.Compile([]byte(tt.pattern)).PrefixMatches([]byte(tt.s))
			if !slices.Equal(got, tt.want) {
				t.Errorf("PrefixMatches(%q, %q) = %v, want %v", tt.pattern, tt.s, got, tt.want)
			}
		})
	}
}

func TestSuffixMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		s       string
		want    []int
	}{
		{name: "literal", pattern: "bc", s: "abc", want: []int{1}},
		{name: "literal absent", pattern: "x", s: "abc", want: nil},
		{name: "empty pattern", pattern: "", s: "abc", want: []int{3}},
		{name: "star every suffix", pattern: "*", s: "abc", want: []int{0, 1, 2, 3}},
		{name: "star leading", pattern: "*b", s: "abcb", want: []int{0, 1, 2, 3}},
		{name: "question rune", pattern: "?", s: "xé", want: []int{1}},
		{name: "class", pattern: "[bc]", s: "abc", want: []int{2}},
		{name: "run absent", pattern: "a*b", s: "xyz", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := glob.Compile([]byte(tt.pattern)).SuffixMatches([]byte(tt.s))
			if !slices.Equal(got, tt.want) {
				t.Errorf("SuffixMatches(%q, %q) = %v, want %v", tt.pattern, tt.s, got, tt.want)
			}
		})
	}
}

func TestFindAll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		s       string
		want    [][2]int
	}{
		{name: "literal repeated", pattern: "a", s: "aba", want: [][2]int{{0, 1}, {2, 3}}},
		{name: "literal absent", pattern: "x", s: "abc", want: nil},
		{name: "star whole", pattern: "*", s: "abc", want: [][2]int{{0, 3}}},
		{name: "star middle longest", pattern: "a*b", s: "aXbYb", want: [][2]int{{0, 5}}},
		{name: "question runes", pattern: "?", s: "éa", want: [][2]int{{0, 2}, {2, 3}}},
		{name: "question lone continuation bytes", pattern: "?", s: "\x80\x80", want: [][2]int{{0, 1}, {1, 2}}},
		{name: "literal continuation byte", pattern: "\x82", s: "€", want: [][2]int{{1, 2}}},
		{name: "class continuation byte agrees with literal", pattern: "[\x82]", s: "€", want: [][2]int{{1, 2}}},
		{name: "empty value", pattern: "*", s: "", want: nil},
		{name: "empty pattern", pattern: "", s: "abc", want: nil},
		{name: "run absent", pattern: "a*b", s: "xyz", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := glob.Compile([]byte(tt.pattern)).FindAll([]byte(tt.s))
			if !slices.Equal(got, tt.want) {
				t.Errorf("FindAll(%q, %q) = %v, want %v", tt.pattern, tt.s, got, tt.want)
			}
		})
	}
}
