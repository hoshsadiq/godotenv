// Package bashdiff extracts parameter-expansion cases from GNU bash's
// tests/*.tests files so this fork's parser can be diffed against the real
// shell.
//
// It is test-support code: it never executes shell input, has no third-party
// dependencies and is regexp-free, mirroring the parser's design constraints.
// Only the structure of the .tests files is read; the GPL text itself is never
// copied into this repository.
package bashdiff

import (
	"fmt"
	"strings"
)

// Source records which shell construct a Case was derived from.
type Source uint8

const (
	// SourceBare is a plain `VAR=rhs` assignment line.
	SourceBare Source = iota
	// SourceRechoQuoted is `recho "single arg"`.
	SourceRechoQuoted
	// SourceRechoToken is `recho token` with no unquoted whitespace.
	SourceRechoToken
	// SourceEcho is an `echo <args>` command in a POSIX suite.
	SourceEcho
	// SourceArgv is an `argv.py <args>` command in a POSIX suite.
	SourceArgv
	// SourceSetDash is a FreeBSD `set -- <expr>` testcase.
	SourceSetDash
	// SourceCheck is a NetBSD `check <expr> <expected>` assertion.
	SourceCheck
)

// String returns the human-readable source name.
func (s Source) String() string {
	switch s {
	case SourceBare:
		return "bare"
	case SourceRechoQuoted:
		return "recho-quoted"
	case SourceRechoToken:
		return "recho-token"
	case SourceEcho:
		return "echo"
	case SourceArgv:
		return "argv"
	case SourceSetDash:
		return "set--"
	case SourceCheck:
		return "check"
	default:
		return "unknown"
	}
}

// Case is a single expansion expression to compare between godotenv and a
// reference shell suite. It is deliberately suite-agnostic: the bash .tests
// extractor and the POSIX-suite extractors all produce Cases, and the runner
// only cares about Expr, Context and (optionally) Expected.
type Case struct {
	ID      string   // stable id: file:line:VAR
	File    string   // source file name
	Line    int      // 1-based line number
	Var     string   // variable whose value is compared
	Expr    string   // .env assignment line, e.g. OUT="$xxx"
	Context []string // earlier plain assignments to prepend
	Source  Source

	// Expected is the recorded expected value for suites that carry in-tree
	// expectations (Oils, FreeBSD, NetBSD). The bash suite leaves it empty and
	// computes the value by running the reference shell.
	Expected    string
	HasExpected bool

	// Adjudicate marks a POSIX case whose expectation comes from a dash-specific
	// override. The runner confirms it against /bin/dash and skips the case
	// when dash is unavailable.
	Adjudicate bool

	// NoGlob marks a FreeBSD case run under `set -f`, where the shell performed
	// no pathname expansion on the result.
	NoGlob bool

	// Skip, when non-empty, is the reason the case is not compared.
	Skip string
}

// FileCounts reports extraction statistics for one .tests file.
type FileCounts struct {
	File     string
	Lines    int // total lines seen
	Cases    int // cases emitted
	Bare     int // SourceBare cases
	Recho    int // SourceRecho* cases
	Excluded int // candidate lines rejected by the exclusion rules
	Contexts int // assignments tracked as context
}

const (
	qNone = iota
	qSingle
	qDouble
)

// scanQuotes returns the quoting state (qNone/qSingle/qDouble) of every byte in
// s, plus whether all quotes are balanced. A backslash escapes the next byte in
// unquoted and double-quoted contexts, matching shell rules.
func scanQuotes(s string) ([]byte, bool) {
	states := make([]byte, len(s))
	state := byte(qNone)
	skip := false
	for i := range len(s) {
		if skip {
			skip = false
			continue
		}
		c := s[i]
		states[i] = state
		switch state {
		case qNone:
			switch c {
			case '\\':
				if i+1 < len(s) {
					states[i+1] = qNone
					skip = true
				}
			case '\'':
				state = qSingle
			case '"':
				state = qDouble
			}
		case qSingle:
			if c == '\'' {
				state = qNone
			}
		case qDouble:
			switch c {
			case '\\':
				if i+1 < len(s) {
					states[i+1] = qDouble
					skip = true
				}
			case '"':
				state = qNone
			}
		}
	}
	return states, state == qNone
}

// codeView returns s with single-quoted bytes masked, so substring checks only
// see bytes that the shell would actually interpret.
func codeView(s string, states []byte) string {
	b := []byte(s)
	for i := range b {
		if states[i] == qSingle {
			b[i] = 0
		}
	}
	return string(b)
}

// hasUnquotedSpace reports whether s contains whitespace outside quotes.
func hasUnquotedSpace(s string, states []byte) bool {
	for i := range len(s) {
		if states[i] == qNone && (s[i] == ' ' || s[i] == '\t') {
			return true
		}
	}
	return false
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// splitAssignment parses a `NAME=rhs` line. It returns false for anything that
// is not a single, syntactically valid shell assignment (so `export FOO=bar`,
// `declare -i x=1`, arrays and multi-assignment lines are rejected).
func splitAssignment(line string) (name, rhs string, ok bool) {
	eq := -1
	for i := range len(line) {
		c := line[i]
		if c == '=' {
			eq = i
			break
		}
		if i == 0 {
			if !isIdentStart(c) {
				return "", "", false
			}
			continue
		}
		if !isIdentChar(c) {
			return "", "", false
		}
	}
	if eq <= 0 {
		return "", "", false
	}
	return line[:eq], line[eq+1:], true
}

// isPlainLiteral reports whether rhs is a bare literal with no expansion or
// quoting, and so is safe to reuse as parser context.
func isPlainLiteral(rhs string) bool {
	for i := range len(rhs) {
		c := rhs[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_', c == '.', c == '/', c == ':', c == '+', c == '-':
		default:
			return false
		}
	}
	return true
}

// badSubstrings are constructs that are out of scope for the differential. They
// are rejected rather than allowlisted so that quoting, backslash and pattern
// differences still surface as failures.
var badSubstrings = []string{
	"`", "$(", "$[", "${!", "[[", "((", "(", ")", ">", "<",
	";", "&", "|", "IFS",
	"export ", "declare ", "local ", "readonly ", "typeset ",
}

// shellMaintainedVars are variables bash sets or maintains itself and that an
// empty environment cannot neutralise (PWD, PATH, RANDOM, ...). Comparing them
// would measure the environment, not the parser, so they are out of scope.
var shellMaintainedVars = map[string]bool{
	"PWD": true, "OLDPWD": true, "SHLVL": true, "PATH": true, "HOME": true,
	"RANDOM": true, "SECONDS": true, "LINENO": true, "FUNCNAME": true,
	"BASHPID": true, "PPID": true, "EPOCHREALTIME": true, "EPOCHSECONDS": true,
	"SRANDOM": true, "BASH": true, "BASH_VERSION": true, "BASH_VERSINFO": true,
	"BASH_SOURCE": true, "BASH_LINENO": true, "BASH_SUBSHELL": true,
	"BASH_ARGC": true, "BASH_ARGV": true, "BASH_COMMAND": true,
	"BASH_EXECUTION_STRING": true, "BASH_REMATCH": true, "BASHOPTS": true,
	"SHELLOPTS": true, "HISTCMD": true, "DIRSTACK": true, "PIPESTATUS": true,
	"GROUPS": true, "HOSTNAME": true, "HOSTTYPE": true, "MACHTYPE": true,
	"OSTYPE": true, "COMP_WORDS": true, "COMP_CWORD": true, "COMP_LINE": true,
	"COMP_POINT": true, "COMP_KEY": true, "COMP_TYPE": true, "EUID": true,
	"UID": true, "GID": true, "_": true, "IFS": true,
	// Variables the bash test harness sets for itself.
	"THIS_SH": true, "TESTDIR": true, "TESTSHELL": true,
}

// isExcluded reports whether the value expression v contains a construct the
// harness does not attempt to model.
func isExcluded(v string, states []byte) bool {
	code := codeView(v, states)
	for _, bad := range badSubstrings {
		if strings.Contains(code, bad) {
			return true
		}
	}
	// Raw control bytes are not a meaningful .env value.
	for i := range len(v) {
		if (v[i] < 0x20 && v[i] != '\t') || v[i] == 0x7f {
			return true
		}
	}
	// Case-modifying expansion ${name^} / ${name,} / ${name~}.
	if strings.Contains(code, "^}") || strings.Contains(code, ",}") || strings.Contains(code, "~}") {
		return true
	}
	if hasParamAssignment(code) || hasSpecialParam(code) || hasShellVar(code) {
		return true
	}
	// Tilde expansion only happens on unquoted tildes.
	for i := range len(v) {
		if v[i] == '~' && states[i] == qNone {
			return true
		}
	}
	return false
}

func hasShellVar(code string) bool {
	for i := range len(code) {
		if code[i] != '$' {
			continue
		}
		j := i + 1
		if j >= len(code) {
			continue
		}
		if code[j] == '{' {
			j++
			if j < len(code) && code[j] == '#' {
				j++
			}
		}
		start := j
		for j < len(code) && isIdentChar(code[j]) {
			j++
		}
		if j > start && shellMaintainedVars[code[start:j]] {
			return true
		}
	}
	return false
}

// hasParamAssignment reports whether code contains ${name=word}, ${name:=word}
// or an array subscript ${name[...]}.
func hasParamAssignment(code string) bool {
	for i := 0; i+2 < len(code); i++ {
		if code[i] != '$' || code[i+1] != '{' {
			continue
		}
		j := i + 2
		if j < len(code) && code[j] == '#' {
			j++
		}
		for j < len(code) && isIdentChar(code[j]) {
			j++
		}
		if j >= len(code) {
			continue
		}
		switch code[j] {
		case '=', '[', '@', '^', ',', '~':
			return true
		case ':':
			if j+1 < len(code) && code[j+1] == '=' {
				return true
			}
		}
	}
	return false
}

// hasSpecialParam reports whether code references a positional or special
// parameter ($@, $*, $#, $?, $!, $1..$9 and their ${...} forms).
func hasSpecialParam(code string) bool {
	for i := range len(code) {
		if code[i] != '$' {
			continue
		}
		if i+1 >= len(code) {
			continue
		}
		next := code[i+1]
		if next == '@' || next == '*' || next == '#' || next == '?' || next == '!' || next == '$' || next == '-' {
			return true
		}
		if next >= '0' && next <= '9' {
			return true
		}
		if next != '{' {
			continue
		}
		j := i + 2
		if j >= len(code) {
			continue
		}
		if code[j] == '#' {
			j++
			if j >= len(code) || !isIdentStart(code[j]) {
				return true
			}
			continue
		}
		c := code[j]
		if c == '@' || c == '*' || c == '#' || c == '?' || c == '!' {
			return true
		}
		if c >= '0' && c <= '9' {
			return true
		}
	}
	return false
}

// rechoArg extracts the single argument of a `recho` line. It returns the
// argument, whether it is one whole double-quoted string, and whether the line
// is usable at all. Lines with unquoted whitespace (multiple arguments) or
// unbalanced quotes are rejected.
func rechoArg(line string) (arg string, quoted, ok bool) {
	if !strings.HasPrefix(line, "recho") {
		return "", false, false
	}
	rest := line[len("recho"):]
	if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return "", false, false
	}
	rest = strings.TrimLeft(rest, " \t")
	if rest == "" {
		return "", false, false
	}
	states, balanced := scanQuotes(rest)
	if !balanced || hasUnquotedSpace(rest, states) {
		return "", false, false
	}
	quoted = rest[0] == '"' && len(rest) >= 2 && rest[len(rest)-1] == '"' && states[len(rest)-1] == qDouble
	return rest, quoted, true
}

// heredocDelim returns the delimiter of a `<<` heredoc introduced on line, if
// any. Heredoc bodies are skipped rather than parsed.
func heredocDelim(line string) (string, bool) {
	i := strings.Index(line, "<<")
	if i < 0 {
		return "", false
	}
	j := i + 2
	if j < len(line) && line[j] == '<' { // herestring
		return "", false
	}
	if j < len(line) && line[j] == '-' {
		j++
	}
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	if j >= len(line) {
		return "", false
	}
	if line[j] == '\'' || line[j] == '"' {
		q := line[j]
		k := j + 1
		for k < len(line) && line[k] != q {
			k++
		}
		if k < len(line) {
			return line[j+1 : k], true
		}
		return "", false
	}
	k := j
	for k < len(line) && isIdentChar(line[k]) {
		k++
	}
	if k == j {
		return "", false
	}
	return line[j:k], true
}

// Extract parses a bash .tests file into comparison cases, tracking earlier
// plain assignments as context. It returns the cases and per-file counts.
func Extract(fileName string, src []byte) ([]Case, FileCounts) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	counts := FileCounts{File: fileName, Lines: len(lines)}

	var (
		cases   []Case
		context []string
		heredoc string
	)

	for idx, raw := range lines {
		lineNo := idx + 1
		line := strings.TrimRight(raw, "\r")

		if heredoc != "" {
			if strings.TrimSpace(line) == heredoc {
				heredoc = ""
			}
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if delim, ok := heredocDelim(line); ok {
			heredoc = delim
			continue
		}

		if name, rhs, ok := splitAssignment(trimmed); ok {
			states, balanced := scanQuotes(rhs)
			if shellMaintainedVars[name] || !balanced || hasUnquotedSpace(rhs, states) || isExcluded(rhs, states) {
				counts.Excluded++
				continue
			}
			cases = append(cases, Case{
				ID:      fmt.Sprintf("%s:%d:%s", fileName, lineNo, name),
				File:    fileName,
				Line:    lineNo,
				Var:     name,
				Expr:    trimmed,
				Context: append([]string(nil), context...),
				Source:  SourceBare,
			})
			counts.Cases++
			counts.Bare++
			if isPlainLiteral(rhs) {
				context = append(context, trimmed)
				counts.Contexts++
			}
			continue
		}

		arg, quoted, ok := rechoArg(trimmed)
		if !ok {
			continue
		}
		states, _ := scanQuotes(arg)
		if isExcluded(arg, states) {
			counts.Excluded++
			continue
		}
		c := Case{
			ID:      fmt.Sprintf("%s:%d:OUT", fileName, lineNo),
			File:    fileName,
			Line:    lineNo,
			Var:     "OUT",
			Expr:    "OUT=" + arg,
			Context: append([]string(nil), context...),
			Source:  SourceRechoToken,
		}
		if quoted {
			c.Source = SourceRechoQuoted
		}
		cases = append(cases, c)
		counts.Cases++
		counts.Recho++
	}

	return cases, counts
}
