package bashdiff

import (
	_ "embed"
	"strings"
)

//go:embed allowlist.txt
var bashAllowlistText string

//go:embed allowlist_posix.txt
var posixAllowlistText string

// AllowlistEntry is one documented intentional deviation.
type AllowlistEntry struct {
	Key    string // substring matched against a case ID or expression
	Reason string
}

// BashAllowlist returns the bash-mode allowlist.
func BashAllowlist() []AllowlistEntry {
	return parseAllowlist(bashAllowlistText)
}

// POSIXAllowlist returns the default POSIX-mode allowlist.
func POSIXAllowlist() []AllowlistEntry {
	return parseAllowlist(posixAllowlistText)
}

func parseAllowlist(text string) []AllowlistEntry {
	var entries []AllowlistEntry
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, reason, _ := strings.Cut(line, "\t")
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		entries = append(entries, AllowlistEntry{Key: key, Reason: strings.TrimSpace(reason)})
	}
	return entries
}

// Allowlisted reports whether c matches an allowlist entry and, if so, why. An
// entry matches when its key is a substring of the case ID or of the case
// expression.
func Allowlisted(c Case, entries []AllowlistEntry) (reason string, ok bool) {
	for _, e := range entries {
		if strings.Contains(c.ID, e.Key) || strings.Contains(c.Expr, e.Key) {
			return e.Reason, true
		}
	}
	return "", false
}
