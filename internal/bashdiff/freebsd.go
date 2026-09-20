package bashdiff

import (
	"fmt"
	"strconv"
	"strings"
)

// FreeBSDCounts reports extraction statistics for one FreeBSD sh test file.
type FreeBSDCounts struct {
	File     string
	Cases    int // testcase/assertion candidates seen
	Emitted  int
	Compared int
	Skipped  int
	Rejected int
}

// ExtractFreeBSD parses a FreeBSD bin/sh test file. Two shapes are handled:
// `testcase 'set -- <expr>' '<argc>|<value>'` triples and simple
// `[ "<expr>" = <literal> ]` assertions.
func ExtractFreeBSD(fileName string, src []byte) ([]Case, FreeBSDCounts) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	counts := FreeBSDCounts{File: fileName}

	var (
		cases   []Case
		context []string
		inFunc  bool
		noglob  bool
	)
	for i, raw := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(raw)
		switch {
		case trimmed == "testcase() {":
			inFunc = true
			continue
		case inFunc:
			if trimmed == "}" {
				inFunc = false
			}
			continue
		case strings.HasPrefix(trimmed, "testcase "):
			counts.Cases++
			c, ok := freebsdTestcase(fileName, lineNo, context, trimmed, noglob)
			if !ok {
				counts.Rejected++
				continue
			}
			cases = append(cases, c)
		case trimmed == "set -f":
			noglob = true
		case trimmed == "set +f":
			noglob = false
		case strings.HasPrefix(trimmed, "["):
			counts.Cases++
			if c, ok := freebsdAssertion(fileName, lineNo, context, trimmed); ok {
				cases = append(cases, c)
			} else {
				counts.Rejected++
			}
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
		default:
			context = appendContext(context, trimmed)
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

// appendContext adds every simple assignment word on line to the context,
// ignoring lines that are not pure assignments.
func appendContext(context []string, line string) []string {
	words := splitWords(line)
	if len(words) == 0 {
		return context
	}
	var added []string
	for _, w := range words {
		name, rhs, ok := splitAssignment(w)
		if !ok || shellMaintainedVars[name] {
			return context
		}
		states, balanced := scanQuotes(rhs)
		if !balanced || hasUnquotedSpace(rhs, states) || posixExcluded(rhs, states) {
			return context
		}
		added = append(added, w)
	}
	return append(context, added...)
}

func freebsdTestcase(fileName string, lineNo int, context []string, line string, noglob bool) (Case, bool) {
	words := shellWords(strings.TrimPrefix(line, "testcase"))
	if len(words) != 2 {
		return Case{}, false
	}
	code, expected := words[0], words[1]
	const prefix = "set -- "
	if !strings.HasPrefix(code, prefix) {
		return Case{}, false
	}
	expr := strings.TrimSpace(strings.TrimPrefix(code, prefix))
	states, balanced := scanQuotes(expr)
	if !balanced || posixExcluded(expr, states) {
		return Case{}, false
	}

	argcStr, rest, found := strings.Cut(expected, "|")
	if !found {
		return Case{}, false
	}
	argc, err := strconv.Atoi(argcStr)
	if err != nil || argc < 0 {
		return Case{}, false
	}
	if argc == 0 && rest != "" {
		return Case{}, false
	}
	id := fmt.Sprintf("%s:%d:OUT", fileName, lineNo)
	c := Case{
		ID: id, File: fileName, Line: lineNo, Var: "OUT",
		Expr: "OUT=" + expr, Context: append([]string(nil), context...),
		Source: SourceSetDash, HasExpected: true, NoGlob: noglob,
	}
	switch {
	case argc > 1:
		c.Skip = "word splitting produces multiple fields, which dotenv does not model"
		return c, true
	case argc == 1 && strings.ContainsAny(rest, "*?["):
		if !noglob {
			c.Skip = "pathname expansion would glob the result, which dotenv does not model"
			return c, true
		}
		c.Expected = rest
		return c, true
	default:
		c.Expected = rest
		return c, true
	}
}

// freebsdAssertion parses `[ "<expr>" = <literal> ]` assertions. The literal
// side must be free of expansions, and every variable the expression reads must
// already be defined in context.
func freebsdAssertion(fileName string, lineNo int, context []string, line string) (Case, bool) {
	open := strings.Index(line, "[ ")
	close := strings.LastIndex(line, " ]")
	if open != 0 || close < 0 {
		return Case{}, false
	}
	inner := line[len("[ "):close]
	lhs, rhs, ok := strings.Cut(inner, " = ")
	if !ok {
		return Case{}, false
	}
	lhs = strings.TrimSpace(lhs)
	rhs = strings.TrimSpace(rhs)
	if len(lhs) < 2 || lhs[0] != '"' || lhs[len(lhs)-1] != '"' {
		return Case{}, false
	}
	expr := lhs[1 : len(lhs)-1]
	if strings.ContainsAny(rhs, "$`") {
		return Case{}, false
	}
	rhs = strings.Trim(rhs, "'\"")
	if rhs == "" {
		return Case{}, false
	}
	states, balanced := scanQuotes(expr)
	if !balanced || posixExcluded(expr, states) {
		return Case{}, false
	}
	defined := definedVars(context)
	for _, v := range referencedVars(expr) {
		if !defined[v] {
			return Case{}, false
		}
	}
	return Case{
		ID: fmt.Sprintf("%s:%d:OUT", fileName, lineNo), File: fileName, Line: lineNo,
		Var: "OUT", Expr: "OUT=" + lhs, Context: append([]string(nil), context...),
		Source: SourceSetDash, Expected: rhs, HasExpected: true,
	}, true
}

// definedVars returns the names assigned by the context lines.
func definedVars(context []string) map[string]bool {
	names := make(map[string]bool, len(context))
	for _, line := range context {
		if name, _, ok := splitAssignment(line); ok {
			names[name] = true
		}
	}
	return names
}

// referencedVars returns every $name / ${name...} the expression reads.
func referencedVars(expr string) []string {
	var names []string
	for i := range len(expr) {
		if expr[i] != '$' || i+1 >= len(expr) {
			continue
		}
		j := i + 1
		if expr[j] == '{' {
			j++
			if j < len(expr) && expr[j] == '#' {
				j++
			}
		}
		start := j
		for j < len(expr) && isIdentChar(expr[j]) {
			j++
		}
		if j > start {
			names = append(names, expr[start:j])
		}
	}
	return names
}

// shellWords splits s into shell words, removing quotes and backslashes the way
// the shell does when it passes arguments to a command.
func shellWords(s string) []string {
	var (
		words []string
		b     strings.Builder
		in    bool
	)
	flush := func() {
		if in {
			words = append(words, b.String())
			b.Reset()
			in = false
		}
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case ' ', '\t':
			flush()
			i++
		case '\'':
			in = true
			i++
			for i < len(s) && s[i] != '\'' {
				b.WriteByte(s[i])
				i++
			}
			i++
		case '"':
			in = true
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				b.WriteByte(s[i])
				i++
			}
			i++
		case '\\':
			in = true
			if i+1 < len(s) {
				b.WriteByte(s[i+1])
				i += 2
			} else {
				i++
			}
		default:
			in = true
			b.WriteByte(c)
			i++
		}
	}
	flush()
	return words
}
