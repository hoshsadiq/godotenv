package godotenv

import (
	"bytes"
	"unicode/utf8"
)

func findMatch(pattern, str []byte, anchor byte) (start, end int, ok bool) {
	if !bytes.ContainsAny(pattern, "*?[\\") {
		switch anchor {
		case '#':
			if bytes.HasPrefix(str, pattern) {
				return 0, len(pattern), true
			}
		case '%':
			if bytes.HasSuffix(str, pattern) {
				return len(str) - len(pattern), len(str), true
			}
		default:
			if i := bytes.Index(str, pattern); i >= 0 {
				return i, i + len(pattern), true
			}
		}

		return 0, 0, false
	}

	switch anchor {
	case '#':
		for e := len(str); e >= 0; e-- {
			if matchGlob(pattern, str[:e]) {
				return 0, e, true
			}
		}
	case '%':
		for e := len(str); e >= 0; e-- {
			if matchGlob(pattern, str[len(str)-e:]) {
				return len(str) - e, len(str), true
			}
		}
	default:
		for s := 0; s <= len(str); s++ {
			for e := len(str); e >= s; e-- {
				if matchGlob(pattern, str[s:e]) {
					return s, e, true
				}
			}
		}
	}
	return 0, 0, false
}

// matchGlob reports whether the shell glob pattern matches s. It supports `*`,
// `?`, character classes (`[...]`, `[!...]`/`[^...]`) and `\` escapes.
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
