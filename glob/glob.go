// Package glob implements the shell pattern matching used by the parameter
// expansion operators ${v#p}, ${v##p}, ${v%p} and ${v%%p}, and by the bash
// replacement family ${v/p/r} and ${v//p/r}.
//
// A pattern is a byte string. The following bytes are special:
//
//   - '*' matches any sequence of bytes, including the empty sequence.
//   - '?' matches exactly one UTF-8 rune.
//   - '[...]' matches one byte drawn from a class. A leading '!' or '^'
//     negates the class, 'a-z' is an inclusive byte range, and a ']' that
//     immediately follows the opening '[' (or its negation) is a literal
//     member rather than the terminator. An unterminated class is not a class:
//     its '[' matches a literal '[' and the remaining bytes are literal.
//   - '\x' matches the literal byte x, so '\*' is a literal asterisk and '\\'
//     a literal backslash. A trailing '\' is a literal backslash.
//
// Matching is byte-oriented except for '?', which consumes a whole rune. A
// pattern can therefore match a prefix that ends in the middle of a multi-byte
// rune; callers that need rune-aligned results must choose rune-aligned
// candidate boundaries themselves.
//
// The implementation is regexp-free and never touches the filesystem. It
// deliberately does not use path.Match or filepath.Match, which are
// separator-aware and explicitly non-POSIX (golang/go#23456), nor
// filepath.Glob or fs.Glob, which enumerate the filesystem.
package glob

import (
	"bytes"
	"unicode/utf8"
)

// Match reports whether the shell glob pattern matches s in full.
func Match(pattern, s []byte) bool {
	if !globMaybeMatch(pattern, s) {
		return false
	}

	return matchGlob(pattern, s)
}

// MatchPrefix returns the longest match of pattern anchored at the start of s,
// as the half-open byte range [start, end), and whether any prefix matches.
// start is always 0.
func MatchPrefix(pattern, s []byte) (start, end int, ok bool) {
	n, ok := matchPrefixLen(pattern, s)
	if !ok {
		return 0, 0, false
	}

	return 0, n, true
}

func matchPrefixLen(pattern, s []byte) (int, bool) {
	if !globMaybeMatch(pattern, s) {
		return 0, false
	}

	if !bytes.ContainsAny(pattern, "*?[\\") {
		if bytes.HasPrefix(s, pattern) {
			return len(pattern), true
		}

		return 0, false
	}

	// A single unescaped '*' with no '?', '[' or '\' has a direct
	// longest-prefix answer.
	if star := bytes.IndexByte(pattern, '*'); star >= 0 &&
		!bytes.ContainsAny(pattern, "?[\\") &&
		bytes.IndexByte(pattern[star+1:], '*') < 0 {
		pre, suf := pattern[:star], pattern[star+1:]
		switch {
		case len(pre) == 0 && len(suf) == 0:
			return len(s), true
		case len(pre) == 0:
			if i := bytes.LastIndex(s, suf); i >= 0 {
				return i + len(suf), true
			}

			return 0, false
		case len(suf) == 0:
			if bytes.HasPrefix(s, pre) {
				return len(s), true
			}

			return 0, false
		default:
			if bytes.HasPrefix(s, pre) {
				if i := bytes.LastIndex(s[len(pre):], suf); i >= 0 {
					return len(pre) + i + len(suf), true
				}
			}

			return 0, false
		}
	}

	for n := len(s); n >= 0; n-- {
		if matchGlob(pattern, s[:n]) {
			return n, true
		}
	}

	return 0, false
}

// MatchSuffix returns the longest match of pattern anchored at the end of s, as
// the half-open byte range [start, end), and whether any suffix matches. end is
// always len(s).
func MatchSuffix(pattern, s []byte) (start, end int, ok bool) {
	n, ok := matchSuffixLen(pattern, s)
	if !ok {
		return 0, 0, false
	}

	return len(s) - n, len(s), true
}

func matchSuffixLen(pattern, s []byte) (int, bool) {
	if !globMaybeMatch(pattern, s) {
		return 0, false
	}

	if !bytes.ContainsAny(pattern, "*?[\\") {
		if bytes.HasSuffix(s, pattern) {
			return len(pattern), true
		}

		return 0, false
	}

	for n := len(s); n >= 0; n-- {
		if matchGlob(pattern, s[len(s)-n:]) {
			return n, true
		}
	}

	return 0, false
}

// Find returns the longest match of pattern anywhere in s as the half-open byte
// range [start, end), and whether any match exists. When several matches share
// the longest length, the one starting earliest wins.
func Find(pattern, s []byte) (start, end int, ok bool) {
	if !globMaybeMatch(pattern, s) {
		return 0, 0, false
	}

	if !bytes.ContainsAny(pattern, "*?[\\") {
		if i := bytes.Index(s, pattern); i >= 0 {
			return i, i + len(pattern), true
		}

		return 0, 0, false
	}

	for start := 0; start <= len(s); start++ {
		for end := len(s); end >= start; end-- {
			if matchGlob(pattern, s[start:end]) {
				return start, end, true
			}
		}
	}

	return 0, 0, false
}

// matchGlob reports whether the shell glob pattern matches s in full. It
// supports '*', '?', character classes ('[...]', '[!...]'/'[^...]') and '\'
// escapes.
func matchGlob(pattern, s []byte) bool {
	pi, si := 0, 0
	star := -1
	starS := 0

	for si < len(s) {
		switch {
		case pi < len(pattern) && pattern[pi] == '\\' && pi+1 < len(pattern):
			if pattern[pi+1] != s[si] {
				if star < 0 {
					return false
				}
				pi = star + 1
				starS++
				si = starS
				continue
			}
			pi += 2
			si++
		case pi < len(pattern) && pattern[pi] == '*':
			star = pi
			starS = si
			pi++
		case pi < len(pattern) && pattern[pi] == '?':
			_, size := utf8.DecodeRune(s[si:])
			pi++
			si += size
		case pi < len(pattern) && pattern[pi] == '[':
			next, matched, ok := matchClass(pattern, pi, s[si])
			switch {
			case ok && matched:
				pi = next
				si++
			case !ok && s[si] == '[':
				pi++
				si++
			case star >= 0:
				pi = star + 1
				starS++
				si = starS
			default:
				return false
			}
		case pi < len(pattern) && pattern[pi] == s[si]:
			pi++
			si++
		default:
			if star < 0 {
				return false
			}
			pi = star + 1
			starS++
			si = starS
		}
	}

	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}

	return pi == len(pattern)
}

func matchClass(pattern []byte, start int, c byte) (next int, matched, ok bool) {
	i := start + 1
	negate := false
	if i < len(pattern) && (pattern[i] == '^' || pattern[i] == '!') {
		negate = true
		i++
	}

	matched = false
	first := true
	for i < len(pattern) && (pattern[i] != ']' || first) {
		first = false
		if i+2 < len(pattern) && pattern[i+1] == '-' && pattern[i+2] != ']' {
			if pattern[i] <= c && c <= pattern[i+2] {
				matched = true
			}
			i += 3
			continue
		}
		if pattern[i] == c {
			matched = true
		}
		i++
	}

	if i >= len(pattern) {
		return 0, false, false
	}

	return i + 1, matched != negate, true
}

// globMaybeMatch is a cheap necessary condition for a match: every maximal
// literal run in the pattern must occur in s. It lets the no-match wildcard
// cases return without scanning every candidate substring. ok is false from
// globRuns when the pattern cannot be reasoned about, in which case the full
// matcher runs.
func globMaybeMatch(pattern, s []byte) bool {
	runs, ok := globRuns(pattern)
	if !ok {
		return true
	}
	for _, run := range runs {
		if !bytes.Contains(s, run) {
			return false
		}
	}

	return true
}

// globRuns returns the literal substrings that any match of pattern must
// contain, with escapes resolved. It reports ok=false for an unterminated
// character class, whose glob meaning is ambiguous.
func globRuns(pattern []byte) (runs [][]byte, ok bool) {
	var cur []byte
	flush := func() {
		if len(cur) > 0 {
			runs = append(runs, cur)
			cur = nil
		}
	}

	for i := 0; i < len(pattern); {
		switch c := pattern[i]; c {
		case '\\':
			if i+1 < len(pattern) {
				cur = append(cur, pattern[i+1])
				i += 2
			} else {
				cur = append(cur, '\\')
				i++
			}
		case '*', '?':
			flush()
			i++
		case '[':
			end := globClassEnd(pattern, i)
			if end < 0 {
				return nil, false
			}
			flush()
			i = end
		default:
			cur = append(cur, c)
			i++
		}
	}
	flush()

	return runs, true
}

// globClassEnd returns the offset just past the ']' closing the class at start,
// or -1 when the class is unterminated. It mirrors matchClass's scan.
func globClassEnd(pattern []byte, start int) int {
	i := start + 1
	if i < len(pattern) && (pattern[i] == '^' || pattern[i] == '!') {
		i++
	}

	first := true
	for i < len(pattern) && (pattern[i] != ']' || first) {
		first = false
		if i+2 < len(pattern) && pattern[i+1] == '-' && pattern[i+2] != ']' {
			i += 3
			continue
		}
		i++
	}
	if i >= len(pattern) {
		return -1
	}

	return i + 1
}
