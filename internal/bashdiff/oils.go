package bashdiff

import (
	"fmt"
	"strconv"
	"strings"
)

// OilsCounts reports extraction statistics for one Oils spec file.
type OilsCounts struct {
	File     string
	Cases    int // #### headers seen
	Emitted  int // Cases produced (compared + skipped)
	Compared int // Cases carrying a usable stdout expectation
	Skipped  int // Cases produced with a Skip reason
	Rejected int // Cases dropped because the body was not echo-only
}

// oilsExpect is a recorded stdout/status expectation.
type oilsExpect struct {
	stdout    string
	hasStdout bool
	status    int
	hasStatus bool
}

type oilsMeta struct {
	def      oilsExpect
	dashOver oilsExpect
	dashNI   bool
}

type oilsLine struct {
	num  int
	text string
}

// ExtractOils parses an Oils spec .test.sh file into comparison cases. Only
// cases whose body is a sequence of simple assignments followed by one or more
// `echo` commands are extracted; everything else is counted as rejected.
func ExtractOils(fileName string, src []byte) ([]Case, OilsCounts) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	counts := OilsCounts{File: fileName}

	type rawCase struct {
		line  int
		title string
		body  []oilsLine
	}
	var raw []rawCase
	cur := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "#### ") || line == "####" {
			raw = append(raw, rawCase{
				line:  i + 1,
				title: strings.TrimSpace(strings.TrimPrefix(line, "####")),
			})
			cur = len(raw) - 1
			continue
		}
		if cur >= 0 {
			raw[cur].body = append(raw[cur].body, oilsLine{num: i + 1, text: line})
		}
	}

	var cases []Case
	for _, rc := range raw {
		counts.Cases++
		code, meta := parseOilsMeta(rc.body)
		emitted, rejected := oilsCase(fileName, rc.line, rc.title, code, meta)
		if rejected {
			counts.Rejected++
			continue
		}
		cases = append(cases, emitted...)
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

// parseOilsMeta splits a case body into code lines and recorded expectations.
func parseOilsMeta(body []oilsLine) ([]oilsLine, oilsMeta) {
	var (
		code []oilsLine
		m    oilsMeta
	)
	for i := 0; i < len(body); i++ {
		ln := body[i]
		if !strings.HasPrefix(ln.text, "##") {
			code = append(code, ln)
			continue
		}
		rest := strings.TrimPrefix(ln.text, "##")
		rest = strings.TrimPrefix(rest, " ")
		kind, shell, key, value, ok := parseOilsLine(rest)
		if !ok {
			continue
		}
		switch key {
		case "STDOUT":
			var block []string
			j := i + 1
			for j < len(body) && strings.TrimSpace(body[j].text) != "## END" {
				block = append(block, body[j].text)
				j++
			}
			val := strings.Join(block, "\n")
			if len(block) > 0 {
				val += "\n"
			}
			applyOilsExpect(&m, kind, shell, val)
			i = j
		case "stdout":
			applyOilsExpect(&m, kind, shell, strings.TrimSpace(value))
		case "stdout-json":
			val := ""
			if unq, err := strconv.Unquote(strings.TrimSpace(value)); err == nil {
				val = unq
			}
			applyOilsExpect(&m, kind, shell, val)
		case "status":
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				continue
			}
			applyOilsStatus(&m, kind, shell, n)
		case "code":
			if strings.TrimSpace(value) != "" {
				code = append(code, oilsLine{num: ln.num, text: value})
			}
		}
	}
	return code, m
}

// parseOilsLine splits a `## ...` body into the override kind ("" for the
// default expectation), the shell it applies to, the key and its inline value.
func parseOilsLine(rest string) (kind, shell, key, value string, ok bool) {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", "", "", "", false
	}
	first := fields[0]
	switch {
	case first == "OK", first == "N-I", first == "BUG", strings.HasPrefix(first, "BUG-"):
		kind = first
		if len(fields) < 2 {
			return kind, "", "", "", false
		}
		shell = fields[1]
		after := skipFields(rest, 2)
		key, value, _ = strings.Cut(after, ":")
	default:
		key, value, _ = strings.Cut(rest, ":")
	}
	key = strings.TrimSpace(strings.TrimSuffix(key, ":"))
	if key == "" {
		return kind, shell, "", "", false
	}
	return kind, shell, key, value, true
}

// skipFields returns s with its first n whitespace-separated fields removed.
func skipFields(s string, n int) string {
	i := 0
	for f := 0; f < n && i < len(s); f++ {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		for i < len(s) && s[i] != ' ' && s[i] != '\t' {
			i++
		}
	}
	return strings.TrimSpace(s[i:])
}

func applyOilsExpect(m *oilsMeta, kind, shell, value string) {
	if kind == "" && shell == "" {
		m.def.stdout = value
		m.def.hasStdout = true
		return
	}
	if !strings.Contains(shell, "dash") {
		return
	}
	switch kind {
	case "N-I":
		m.dashNI = true
	case "OK":
		m.dashOver.stdout = value
		m.dashOver.hasStdout = true
	}
}

func applyOilsStatus(m *oilsMeta, kind, shell string, n int) {
	if kind == "" && shell == "" {
		m.def.status = n
		m.def.hasStatus = true
		return
	}
	if !strings.Contains(shell, "dash") {
		return
	}
	switch kind {
	case "N-I":
		m.dashNI = true
	case "OK":
		m.dashOver.status = n
		m.dashOver.hasStatus = true
	}
}

// oilsCase turns one parsed case into zero or more comparison cases. It returns
// rejected=true when the case body cannot be modelled as dotenv assignments.
func oilsCase(fileName string, caseLine int, title string, code []oilsLine, m oilsMeta) (cases []Case, rejected bool) {
	if m.dashNI {
		return []Case{{
			ID:   fmt.Sprintf("%s:%d:OUT", fileName, caseLine),
			File: fileName, Line: caseLine, Var: "OUT",
			Skip: "dash does not implement this case",
		}}, false
	}
	if !m.def.hasStdout && !m.dashOver.hasStdout {
		return nil, true
	}
	if status := m.def.status; m.def.hasStatus && status != 0 {
		return nil, true
	}

	var (
		context []string
		echoes  []oilsLine
		seen    bool
	)
	for _, ln := range code {
		trimmed := strings.TrimSpace(ln.text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "echo ") || trimmed == "echo" {
			seen = true
			echoes = append(echoes, oilsLine{num: ln.num, text: trimmed})
			continue
		}
		if seen {
			return nil, true
		}
		name, rhs, ok := splitAssignment(trimmed)
		if !ok || shellMaintainedVars[name] {
			return nil, true
		}
		states, balanced := scanQuotes(rhs)
		if !balanced || hasUnquotedSpace(rhs, states) || posixExcluded(rhs, states) {
			return nil, true
		}
		context = append(context, trimmed)
	}
	if len(echoes) == 0 {
		return nil, true
	}

	expected := m.def
	adjudicate := false
	if m.dashOver.hasStdout || m.dashOver.hasStatus {
		expected = m.def
		if m.dashOver.hasStdout {
			expected.stdout = m.dashOver.stdout
		}
		if m.dashOver.hasStatus {
			expected.status = m.dashOver.status
		}
		adjudicate = true
	}
	if expected.hasStatus && expected.status != 0 {
		return nil, true
	}

	outLines := strings.Split(strings.TrimSuffix(expected.stdout, "\n"), "\n")
	if len(outLines) != len(echoes) {
		return nil, true
	}

	for i, e := range echoes {
		args := strings.TrimSpace(strings.TrimPrefix(e.text, "echo"))
		words := splitWords(args)
		if len(words) > 0 && strings.HasPrefix(words[0], "-") {
			return nil, true
		}
		expr := "OUT=" + joinWords(words)
		states, balanced := scanQuotes(expr)
		if !balanced || posixExcluded(expr, states) {
			return nil, true
		}
		cases = append(cases, Case{
			ID:          fmt.Sprintf("%s:%d:OUT", fileName, e.num),
			File:        fileName,
			Line:        e.num,
			Var:         "OUT",
			Expr:        expr,
			Context:     append([]string(nil), context...),
			Source:      SourceEcho,
			Expected:    outLines[i],
			HasExpected: true,
			Adjudicate:  adjudicate,
		})
	}
	return cases, false
}
