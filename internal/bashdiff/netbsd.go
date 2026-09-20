package bashdiff

import (
	"fmt"
	"strconv"
	"strings"
)

// NetBSDCounts reports extraction statistics for one NetBSD sh test file.
type NetBSDCounts struct {
	File     string
	Cases    int // check invocations seen
	Emitted  int
	Compared int
	Skipped  int
	Rejected int
}

// ExtractNetBSD parses a NetBSD ATF sh test. It recognises the
// `check '<cmd>' '<expected>' <status>` triples used by t_expand.sh and the
// embedded `check "$var" '<expected>'` assertions used by t_varquote.sh.
func ExtractNetBSD(fileName string, src []byte) ([]Case, NetBSDCounts) {
	lines := joinContinuations(strings.ReplaceAll(string(src), "\r\n", "\n"))
	counts := NetBSDCounts{File: fileName}

	var (
		cases    []Case
		context  []string
		lastName string
		lastExpr string
	)
	for i, raw := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "check ") {
			counts.Cases++
			c, ok := netbsdCheck(fileName, lineNo, context, lastName, lastExpr, trimmed)
			if !ok {
				counts.Rejected++
				continue
			}
			cases = append(cases, c)
			continue
		}
		if name, rhs, ok := splitAssignment(trimmed); ok && !shellMaintainedVars[name] {
			states, balanced := scanQuotes(rhs)
			if balanced && !hasUnquotedSpace(rhs, states) && !posixExcluded(rhs, states) {
				context = append(context, trimmed)
				lastName, lastExpr = name, "OUT="+rhs
			}
		}
	}

	for _, c := range cases {
		counts.Emitted++
		if c.Skip != "" {
			counts.Skipped++
		} else {
			counts.Compared++
		}
	}
	return cases, counts
}

func netbsdCheck(fileName string, lineNo int, context []string, lastName, lastExpr, line string) (Case, bool) {
	words := shellWords(strings.TrimPrefix(line, "check"))
	switch len(words) {
	case 2:
		if lastName == "" || words[0] != "$"+lastName {
			return Case{}, false
		}
		states, balanced := scanQuotes(lastExpr)
		if !balanced || posixExcluded(lastExpr, states) {
			return Case{}, false
		}
		return Case{
			ID: fmt.Sprintf("%s:%d:OUT", fileName, lineNo), File: fileName, Line: lineNo,
			Var: "OUT", Expr: lastExpr, Context: append([]string(nil), context...),
			Source: SourceCheck, Expected: words[1], HasExpected: true,
		}, true
	case 3:
		status, err := strconv.Atoi(words[2])
		if err != nil || status != 0 {
			return Case{}, false
		}
		expr, ok := simpleEchoCommand(words[0])
		if !ok {
			return Case{}, false
		}
		expected := words[1]
		if strings.Contains(expected, "\n") {
			return Case{}, false
		}
		states, balanced := scanQuotes(expr)
		if !balanced || posixExcluded(expr, states) {
			return Case{}, false
		}
		return Case{
			ID: fmt.Sprintf("%s:%d:OUT", fileName, lineNo), File: fileName, Line: lineNo,
			Var: "OUT", Expr: expr, Context: append([]string(nil), context...),
			Source: SourceCheck, Expected: expected, HasExpected: true,
		}, true
	default:
		return Case{}, false
	}
}

// simpleEchoCommand turns `x=1; y=2; echo <args>` into an OUT= assignment. It
// returns false when any statement other than a simple assignment or the final
// echo appears.
func simpleEchoCommand(cmd string) (string, bool) {
	stmts := splitUnquoted(cmd, ';')
	if len(stmts) == 0 {
		return "", false
	}
	last := strings.TrimSpace(stmts[len(stmts)-1])
	if !strings.HasPrefix(last, "echo ") {
		return "", false
	}
	for _, stmt := range stmts[:len(stmts)-1] {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, _, ok := splitAssignment(stmt); !ok {
			return "", false
		}
	}
	args := strings.TrimSpace(strings.TrimPrefix(last, "echo"))
	words := splitWords(args)
	if len(words) > 0 && strings.HasPrefix(words[0], "-") {
		return "", false
	}
	return "OUT=" + joinWords(words), true
}

// splitUnquoted splits s on every occurrence of sep outside quotes.
func splitUnquoted(s string, sep byte) []string {
	states, _ := scanQuotes(s)
	var parts []string
	start := 0
	for i := range len(s) {
		if s[i] == sep && states[i] == qNone {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// joinContinuations folds shell `\`-newline continuations into single lines,
// preserving the original line count with empty padding so line numbers remain
// accurate.
func joinContinuations(text string) []string {
	raw := strings.Split(text, "\n")
	out := make([]string, len(raw))
	i := 0
	for i < len(raw) {
		start := i
		line := raw[i]
		for strings.HasSuffix(strings.TrimRight(line, " \t"), "\\") && i+1 < len(raw) {
			line = strings.TrimRight(strings.TrimRight(line, " \t"), "\\")
			i++
			line += " " + strings.TrimSpace(raw[i])
		}
		out[start] = line
		i++
	}
	return out
}
