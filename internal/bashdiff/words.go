package bashdiff

import "strings"

// splitWords splits s into shell words on unquoted whitespace, preserving the
// quoting of each word. It is used to normalise `echo` argument lists: the
// shell joins echo's arguments with a single space, so a run of unquoted
// whitespace in the source collapses to one space in the output.
func splitWords(s string) []string {
	states, _ := scanQuotes(s)
	var words []string
	start := -1
	for i := range len(s) {
		if states[i] == qNone && (s[i] == ' ' || s[i] == '\t') {
			if start >= 0 {
				words = append(words, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		words = append(words, s[start:])
	}
	return words
}

// joinWords joins words with single spaces, the way `echo` joins its arguments.
func joinWords(words []string) string {
	return strings.Join(words, " ")
}
