package bashdiff

import "strings"

// posixExcluded reports whether a POSIX-suite value expression uses a construct
// that is out of scope for the default parser. On top of the bash extractor's
// exclusions it rejects the bash-only $'...' and $"..." quoting forms, which
// the default mode keeps literal.
func posixExcluded(v string, states []byte) bool {
	code := codeView(v, states)
	if strings.Contains(code, "$'") || strings.Contains(code, "$\"") {
		return true
	}
	return isExcluded(v, states)
}

// GlobSkip reports whether the evaluated value would be pathname-expanded by a
// reference shell, which dotenv does not model. It returns the skip reason, or
// the empty string when the case is safe to compare.
func GlobSkip(c Case, actual string) string {
	if c.NoGlob || !hasGlobMeta(actual) {
		return ""
	}
	return "pathname expansion would glob the result, which dotenv does not model"
}

// hasGlobMeta reports whether s contains a pattern metacharacter that a shell
// would try to pathname-expand: `*`, `?` or a valid `[...]` bracket expression.
// An unmatched or empty bracket is literal to the shell and returns false.
func hasGlobMeta(s string) bool {
	for i := range len(s) {
		switch s[i] {
		case '*', '?':
			return true
		case '[':
			j := i + 1
			if j < len(s) && s[j] == ']' {
				j++
			}
			for j < len(s) && s[j] != ']' {
				j++
			}
			if j < len(s) {
				return true
			}
		}
	}
	return false
}
