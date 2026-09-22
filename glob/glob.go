// Package glob implements shell pattern matching over byte strings.
//
// A pattern is a byte string. The following bytes are special:
//
//   - '*' matches any sequence of bytes, including the empty sequence.
//   - '?' matches exactly one character: a whole UTF-8 rune where the bytes
//     decode to one, or a single byte where they do not (a lone or truncated
//     sequence), matching how bash treats malformed input.
//   - '[...]' matches one byte drawn from a class. A leading '!' or '^'
//     negates the class, 'a-z' is an inclusive byte range, and a ']' that
//     immediately follows the opening '[' (or its negation) is a literal
//     member rather than the terminator. An unterminated class is not a class:
//     its '[' matches a literal '[' and the remaining bytes are literal.
//   - '\x' matches the literal byte x, so '\*' is a literal asterisk and '\\'
//     a literal backslash. A trailing '\' is a literal backslash.
//
// Matching is byte-oriented except for '?', which consumes one character and
// only starts at a character boundary, as defined by a greedy left-to-right
// decode. A pattern can therefore match a prefix that ends in the middle of a
// multi-byte rune; callers that need rune-aligned results must choose
// rune-aligned candidate boundaries themselves.
//
// Compile a pattern once and reuse it when matching the same pattern against
// many strings. Matching is a single left-to-right state simulation: the
// pattern is compiled to a token list and each text position carries the
// furthest a token suffix can match. It is O(len(pattern)*len(s)) time and
// O(len(pattern)+len(s)) space, with no backtracking, so a hostile pattern
// cannot blow up the search.
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

const (
	kindLiteral byte = iota
	kindStar
	kindAny
	kindClass
)

// globToken is one compiled element of a pattern.
type globToken struct {
	kind byte
	lit  byte
	cls  int
}

// Pattern is a compiled pattern. Compile it once and reuse it when matching the
// same pattern against many strings; the package-level functions recompile on
// every call.
type Pattern struct {
	pattern []byte
	toks    []globToken
	runs    [][]byte
	runsOK  bool
	hasMeta bool
	hasAny  bool
}

// Compile compiles pattern for matching. The pattern is copied, so later
// changes to the caller's slice have no effect.
func Compile(pattern []byte) *Pattern {
	p := &Pattern{
		pattern: bytes.Clone(pattern),
		hasMeta: bytes.ContainsAny(pattern, "*?[\\"),
		hasAny:  bytes.IndexByte(pattern, '?') >= 0,
	}
	p.toks = tokenize(p.pattern)
	p.runs, p.runsOK = globRuns(p.pattern)

	return p
}

// Match reports whether the shell glob pattern matches s in full.
func Match(pattern, s []byte) bool {
	return Compile(pattern).Match(s)
}

// MatchPrefix returns the longest match of the shell glob pattern anchored at
// the start of s, as the half-open byte range [start, end), and whether any
// prefix matches. start is always 0.
func MatchPrefix(pattern, s []byte) (start, end int, ok bool) {
	return Compile(pattern).MatchPrefix(s)
}

// MatchSuffix returns the longest match of the shell glob pattern anchored at
// the end of s, as the half-open byte range [start, end), and whether any
// suffix matches. end is always len(s).
func MatchSuffix(pattern, s []byte) (start, end int, ok bool) {
	return Compile(pattern).MatchSuffix(s)
}

// Find returns the leftmost match of the shell glob pattern in s as the
// half-open byte range [start, end), and whether any match exists. When more
// than one match starts at that leftmost position, the longest wins.
func Find(pattern, s []byte) (start, end int, ok bool) {
	return Compile(pattern).Find(s)
}

// Match reports whether the pattern matches s in full.
func (p *Pattern) Match(s []byte) bool {
	if !p.maybeMatch(s) {
		return false
	}

	if !p.hasMeta {
		return bytes.Equal(p.pattern, s)
	}

	pre, n := p.matchRow(s)

	return pre[0] == n
}

// MatchPrefix returns the longest match of the pattern anchored at the start of
// s, as the half-open byte range [start, end), and whether any prefix matches.
// start is always 0.
func (p *Pattern) MatchPrefix(s []byte) (start, end int, ok bool) {
	n, ok := p.matchPrefixLen(s)
	if !ok {
		return 0, 0, false
	}

	return 0, n, true
}

func (p *Pattern) matchPrefixLen(s []byte) (int, bool) {
	if !p.maybeMatch(s) {
		return 0, false
	}

	if !p.hasMeta {
		if bytes.HasPrefix(s, p.pattern) {
			return len(p.pattern), true
		}

		return 0, false
	}

	// A single unescaped '*' with no '?', '[' or '\' has a direct
	// longest-prefix answer.
	if star := bytes.IndexByte(p.pattern, '*'); star >= 0 &&
		!bytes.ContainsAny(p.pattern, "?[\\") &&
		bytes.IndexByte(p.pattern[star+1:], '*') < 0 {
		pre, suf := p.pattern[:star], p.pattern[star+1:]
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

	pre, _ := p.matchRow(s)
	if pre[0] < 0 {
		return 0, false
	}

	return pre[0], true
}

// MatchSuffix returns the longest match of the pattern anchored at the end of
// s, as the half-open byte range [start, end), and whether any suffix matches.
// end is always len(s).
func (p *Pattern) MatchSuffix(s []byte) (start, end int, ok bool) {
	n, ok := p.matchSuffixLen(s)
	if !ok {
		return 0, 0, false
	}

	return len(s) - n, len(s), true
}

func (p *Pattern) matchSuffixLen(s []byte) (int, bool) {
	if !p.maybeMatch(s) {
		return 0, false
	}

	if !p.hasMeta {
		if bytes.HasSuffix(s, p.pattern) {
			return len(p.pattern), true
		}

		return 0, false
	}

	pre, n := p.matchRow(s)
	for i := 0; i <= n; i++ {
		if pre[i] == n-i {
			return n - i, true
		}
	}

	return 0, false
}

// Find returns the leftmost match of the pattern in s as the half-open byte
// range [start, end), and whether any match exists. When more than one match
// starts at that leftmost position, the longest wins.
func (p *Pattern) Find(s []byte) (start, end int, ok bool) {
	if !p.maybeMatch(s) {
		return 0, 0, false
	}

	if !p.hasMeta {
		if i := bytes.Index(s, p.pattern); i >= 0 {
			return i, i + len(p.pattern), true
		}

		return 0, 0, false
	}

	pre, n := p.matchRow(s)
	for i := 0; i <= n; i++ {
		if pre[i] >= 0 {
			return i, i + pre[i], true
		}
	}

	return 0, 0, false
}

// FindAll returns every non-overlapping longest match of the pattern in s, left
// to right, as half-open byte ranges. Empty matches are skipped.
func (p *Pattern) FindAll(s []byte) [][2]int {
	var out [][2]int

	if !p.hasMeta {
		if len(p.pattern) == 0 {
			return out
		}
		for off := 0; off <= len(s); {
			i := bytes.Index(s[off:], p.pattern)
			if i < 0 {
				break
			}
			out = append(out, [2]int{off + i, off + i + len(p.pattern)})
			off += i + len(p.pattern)
		}

		return out
	}

	// Scan one byte at a time, so a match may start mid-rune just as the
	// literal path above allows.
	row, n := p.matchRow(s)
	for offset := 0; offset < n; {
		if row[offset] > 0 {
			out = append(out, [2]int{offset, offset + row[offset]})
			offset += row[offset]
			continue
		}
		offset++
	}

	return out
}

// PrefixMatches returns every byte length b for which the pattern matches s[:b]
// in full, in increasing order. Callers that need rune-aligned results can
// filter the lengths to rune boundaries.
func (p *Pattern) PrefixMatches(s []byte) []int {
	if !p.maybeMatch(s) {
		return nil
	}

	if !p.hasMeta {
		if bytes.HasPrefix(s, p.pattern) {
			return []int{len(p.pattern)}
		}

		return nil
	}

	return p.prefixMatches(s)
}

// prefixMatches walks the pattern forward over s, recording every offset at
// which the whole pattern is consumed. '?' can skip several bytes, so the
// reachable states are kept in a ring of positions.
func (p *Pattern) prefixMatches(s []byte) []int {
	n := len(s)
	total := len(p.toks)
	const ring = 5

	var sets [ring][]bool
	for i := range sets {
		sets[i] = make([]bool, total+1)
	}
	sets[0][0] = true

	var sizes []int
	if p.hasAny {
		sizes = charSizes(s)
	}

	var out []int
	for i := 0; i <= n; i++ {
		cur := sets[i%ring]

		// A star may match nothing, so it can be skipped without consuming.
		for t := range total {
			if cur[t] && p.toks[t].kind == kindStar {
				cur[t+1] = true
			}
		}
		if cur[total] {
			out = append(out, i)
		}
		if i == n {
			break
		}

		for t := range total {
			if !cur[t] {
				continue
			}
			switch p.toks[t].kind {
			case kindStar:
				sets[(i+1)%ring][t] = true
			case kindAny:
				if size := sizes[i]; size > 0 {
					sets[(i+size)%ring][t+1] = true
				}
			case kindClass:
				_, matched, _ := matchClass(p.pattern, p.toks[t].cls, s[i])
				if matched {
					sets[(i+1)%ring][t+1] = true
				}
			default:
				if s[i] == p.toks[t].lit {
					sets[(i+1)%ring][t+1] = true
				}
			}
		}

		clear(cur)
	}

	return out
}

// SuffixMatches returns every byte offset i for which the pattern matches s[i:]
// in full, in increasing order. Callers that need rune-aligned results can
// filter the offsets to rune boundaries.
func (p *Pattern) SuffixMatches(s []byte) []int {
	if !p.maybeMatch(s) {
		return nil
	}

	if !p.hasMeta {
		if bytes.HasSuffix(s, p.pattern) {
			return []int{len(s) - len(p.pattern)}
		}

		return nil
	}

	row, n := p.matchRow(s)

	var out []int
	for i := 0; i <= n; i++ {
		if row[i] == n-i {
			out = append(out, i)
		}
	}

	return out
}

// maybeMatch is a cheap necessary condition for a match: every maximal literal
// run in the pattern must occur in s. It lets the no-match wildcard cases
// return without running the full matcher.
func (p *Pattern) maybeMatch(s []byte) bool {
	if !p.runsOK {
		return true
	}
	for _, run := range p.runs {
		if !bytes.Contains(s, run) {
			return false
		}
	}

	return true
}

// tokenize compiles pattern into its elements. An unterminated class becomes a
// literal '[', and a trailing '\' a literal backslash, matching the matcher.
func tokenize(pattern []byte) []globToken {
	toks := make([]globToken, 0, len(pattern))
	noClass := false

	for i := 0; i < len(pattern); {
		switch c := pattern[i]; c {
		case '*':
			toks = append(toks, globToken{kind: kindStar})
			i++
		case '?':
			toks = append(toks, globToken{kind: kindAny})
			i++
		case '[':
			// Once no closing ']' remains, every later '[' is literal
			// too, so scan for the class end only until it first fails.
			if !noClass {
				if end := globClassEnd(pattern, i); end >= 0 {
					toks = append(toks, globToken{kind: kindClass, cls: i})
					i = end
					continue
				}
				noClass = true
			}
			toks = append(toks, globToken{kind: kindLiteral, lit: '['})
			i++
		case '\\':
			if i+1 < len(pattern) {
				toks = append(toks, globToken{kind: kindLiteral, lit: pattern[i+1]})
				i += 2
			} else {
				toks = append(toks, globToken{kind: kindLiteral, lit: '\\'})
				i++
			}
		default:
			toks = append(toks, globToken{kind: kindLiteral, lit: c})
			i++
		}
	}

	return toks
}

// charSizes marks the byte offsets that begin a character under a greedy
// left-to-right decode: sizes[i] is that character's width, or 0 when i sits
// inside one. A byte that cannot start a rune is still a character of its own
// when the preceding bytes decode cleanly, which is how bash matches '?'
// against malformed input.
func charSizes(s []byte) []int {
	sizes := make([]int, len(s))
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRune(s[i:])
		sizes[i] = size
		i += size
	}

	return sizes
}

// matchRow simulates the pattern over s. It returns a row where row[i] is the
// greatest number of bytes the whole pattern can match starting at s[i], or -1
// when the pattern matches no prefix of s[i:]. n is len(s), so row[i] == n-i
// means the pattern matches s[i:] in full. Rows are computed one token at a
// time from the last token backwards, so only two rows of state are live.
func (p *Pattern) matchRow(s []byte) ([]int, int) {
	n := len(s)
	next := make([]int, n+1)
	cur := make([]int, n+1)

	var sizes []int
	if p.hasAny {
		sizes = charSizes(s)
	}

	for t := len(p.toks) - 1; t >= 0; t-- {
		switch p.toks[t].kind {
		case kindStar:
			// The star may match nothing (next[i]) or one more byte
			// (1 + cur[i+1]), so the row fills right to left.
			cur[n] = next[n]
			for i := n - 1; i >= 0; i-- {
				best := next[i]
				if c := cur[i+1]; c >= 0 && c+1 > best {
					best = c + 1
				}
				cur[i] = best
			}
		case kindAny:
			for i := range n {
				size := sizes[i]
				if size == 0 {
					cur[i] = -1
					continue
				}
				if next[i+size] >= 0 {
					cur[i] = size + next[i+size]
				} else {
					cur[i] = -1
				}
			}
			cur[n] = -1
		case kindClass:
			for i := range n {
				// tokenize only emits a class when globClassEnd found
				// the closing ']', so matchClass always reports ok.
				_, matched, _ := matchClass(p.pattern, p.toks[t].cls, s[i])
				if matched && next[i+1] >= 0 {
					cur[i] = 1 + next[i+1]
				} else {
					cur[i] = -1
				}
			}
			cur[n] = -1
		default:
			for i := range n {
				if s[i] == p.toks[t].lit && next[i+1] >= 0 {
					cur[i] = 1 + next[i+1]
				} else {
					cur[i] = -1
				}
			}
			cur[n] = -1
		}

		next, cur = cur, next
	}

	return next, n
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
