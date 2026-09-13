package godotenv

import (
	"bytes"
	"fmt"
	"strconv"
	"unicode"
)

const (
	exportPrefix = "export"
)

type state uint8

const (
	stateKey state = iota
	stateValue
	stateEscapeNone
	stateEscapeSingle
	stateEscapeDouble
	stateQuoteDouble
	stateQuoteSingle
)

// LookupEnvFunc is used to determine the value of an environment, and whether it exists or not.
// This should only look at the application environment and not at previous parsed items in a .env file.
// Previously parsed items in an .env file take precedence over the environment.
type LookupEnvFunc func(name []byte) (value []byte, exists bool)

type parser struct {
	data       []byte
	lineNumber int
	cfg        config
}

func newParser(d []byte, cfg config) *parser {
	return &parser{
		data:       d,
		lineNumber: 1,
		cfg:        cfg,
	}
}

func (p *parser) parse(m map[string]string, lookupEnv LookupEnvFunc) (err error) {
	key := make([]byte, 0, len(p.data))
	value := make([]byte, 0, len(p.data))
	pendingWS := make([]byte, 0, len(p.data))

	state := stateKey

	var j int

	for j = 0; j < len(p.data); j++ {
		c := p.data[j]

		switch state {
		case stateKey:
			switch {
			case c == '=':
				if len(key) == 0 {
					return p.newParserError(j, "empty key")
				}

				state = stateValue
			case c == '#':
				if j == 0 || unicode.IsSpace(rune(p.data[j-1])) {
					nl := bytes.IndexByte(p.data[j+1:], '\n')
					if nl < 0 {
						j = len(p.data)
						continue
					}

					j += nl
					continue
				}

				return p.newParserError(j, "not a valid identifier")
			case c == ' ', c == '\t', c == '\r', c == '\n':
				if bytes.Equal(key, []byte(exportPrefix)) {
					key = key[:0]
				}

				if c == '\n' {
					p.lineNumber++
				}

				// ignore empty space
				if len(key) == 0 {
					continue
				}

				return p.newParserError(j, "unexpected whitespace in key")
			case unicode.IsNumber(rune(c)):
				if len(key) == 0 {
					return p.newParserError(j, "invalid character in key name")
				}
				fallthrough
			case c == '_':
				fallthrough
			case unicode.IsLetter(rune(c)):
				key = append(key, c)
			default:
				return p.newParserError(j, "invalid character in key name")
			}
		case stateValue:
			switch c {
			case '\r':
				// ignore `\r` in an `\r\n`, but not in only `\r`
				if len(p.data) >= j+1 && p.data[j+1] == '\n' {
					continue
				}

				fallthrough
			case '\n':
				p.lineNumber++

				m[string(key)] = string(value)
				key = key[:0]
				value = value[:0]
				pendingWS = pendingWS[:0]
				state = stateKey
			case '\\':
				value = flushPendingWS(value, pendingWS)
				pendingWS = pendingWS[:0]
				state = stateEscapeNone
			case '\'':
				value = flushPendingWS(value, pendingWS)
				pendingWS = pendingWS[:0]
				state = stateQuoteSingle
			case '"':
				value = flushPendingWS(value, pendingWS)
				pendingWS = pendingWS[:0]
				state = stateQuoteDouble
			case '#':
				if unicode.IsSpace(rune(p.data[j-1])) {
					nl := bytes.IndexByte(p.data[j+1:], '\n')
					if nl < 0 {
						j = len(p.data)
						continue
					}

					j += nl
					continue
				}

				value = append(value, c)
			case '$':
				value = flushPendingWS(value, pendingWS)
				pendingWS = pendingWS[:0]
				res, w, err := p.resolveParameter(j, p.data[j+1:], lookupEnv)
				if err != nil {
					return err
				}
				value = append(value, res...)
				j += w
			case ' ', '\t':
				if len(value) == 0 {
					return p.newParserError(j, "unexpected space in value")
				}

				pendingWS = append(pendingWS, c)
			default:
				if c < 32 {
					return p.newInvalidCharacterError(j, c)
				}

				value = flushPendingWS(value, pendingWS)
				pendingWS = pendingWS[:0]
				value = append(value, c)
			}
		case stateEscapeNone:
			if c == '\r' && j+1 < len(p.data) && p.data[j+1] == '\n' {
				p.lineNumber++
				j++
				state = stateValue
				continue
			}

			if c == '\n' {
				p.lineNumber++
				state = stateValue
				continue
			}

			value = append(value, c)
			state = stateValue
		case stateQuoteDouble:
			switch c {
			case '$':
				res, w, err := p.resolveParameter(j, p.data[j+1:], lookupEnv)
				if err != nil {
					return err
				}
				value = append(value, res...)
				j += w
			case '"':
				state = stateValue
			case '\\':
				if p.cfg.posix {
					if j+1 >= len(p.data) {
						return p.newParserError(j, "incomplete escape sequence")
					}
					switch next := p.data[j+1]; next {
					case '$', '`', '"', '\\':
						value = append(value, next)
						j++
					case '\n':
						p.lineNumber++
						j++
					default:
						value = append(value, '\\')
					}
				} else {
					state = stateEscapeDouble
				}
			case '\n':
				p.lineNumber++
				fallthrough
			default:
				value = append(value, c)
			}
		case stateEscapeDouble:
			if c == '\r' && j+1 < len(p.data) && p.data[j+1] == '\n' {
				p.lineNumber++
				j++
				state = stateQuoteDouble
				continue
			}

			if c == '\n' {
				p.lineNumber++
				state = stateQuoteDouble
				continue
			}

			// todo how can we combine some of these cases?
			switch c {
			case 'b':
				value = append(value, '\b')
			case 'f':
				value = append(value, '\f')
			case 'r':
				value = append(value, '\r')
			case 'n':
				value = append(value, '\n')
			case 't':
				value = append(value, '\t')
			case 'u':
				decoded, width := decodeUnicodeEscape(p.data, j)
				value = append(value, decoded...)
				j += width
			default:
				value = append(value, c)
			}

			state = stateQuoteDouble
		case stateQuoteSingle:
			switch c {
			case '\'':
				state = stateValue
			case '\\':
				if p.cfg.posix {
					value = append(value, '\\')
				} else {
					state = stateEscapeSingle
				}
			case '\n':
				p.lineNumber++
				fallthrough
			default:
				value = append(value, c)
			}
		case stateEscapeSingle:
			value = append(value, '\\', c)
			state = stateQuoteSingle
		default:
			panic(fmt.Errorf("state is invalid: %v. THIS IS A BUG", state))
		}
	}

	if state == stateValue {
		m[string(key)] = string(value)
		key = key[:0]
		// value = value[:0]
	}

	switch state {
	case stateValue:
	case stateKey:
		if len(key) != 0 {
			return p.newParserError(j, "missing value operator")
		}
	case stateQuoteDouble:
		return p.newParserError(j, "unmatched double quote")
	case stateQuoteSingle:
		return p.newParserError(j, "unmatched single quote")
	case stateEscapeNone, stateEscapeDouble, stateEscapeSingle: // todo this can be resolved by dealing with the whole input instead of line by line
		return p.newParserError(j, "incomplete escape sequence")
	default:
		panic(fmt.Errorf("state is invalid: %v. THIS IS A BUG", state))
	}

	return nil
}

func flushPendingWS(value, pending []byte) []byte {
	if len(pending) == 0 {
		return value
	}
	return append(value, pending...)
}

// isShellSpecialVar reports whether the character identifies a special
// shell variable such as $*.
func isShellSpecialVar(c uint8) bool {
	switch c {
	case '*', '#', '$', '@', '!', '?', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

// isNum reports whether the byte is an ASCII number.
func isNum(c uint8) bool {
	return '0' <= c && c <= '9'
}

// isAlpha reports whether the byte is an ASCII letter.
func isAlpha(c uint8) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// isAlphaNum reports whether the byte is an ASCII letter, number, or underscore
func isAlphaNum(c uint8) bool {
	return isNum(c) || isAlpha(c) || c == '_'
}

// resolveParameter resolves the parameter starting at s and reports how many
// bytes it consumed. It handles $NAME and the ${NAME<op><word>} forms from the
// POSIX shell parameter expansion rules.
func (p *parser) resolveParameter(characterStart int, s []byte, lookupEnv LookupEnvFunc) (res []byte, skip int, err error) {
	if len(s) == 0 {
		return []byte("$"), 0, nil
	}

	switch {
	case s[0] == '{':
		return p.expandBracedRef(characterStart, s, lookupEnv)
	case p.cfg.restricted:
		return p.resolveName(characterStart, s, lookupEnv)
	case s[0] == '(':
		// Command substitution is never executed. Preserve `$(...)` verbatim
		for i := 1; i < len(s) && s[i] != '\n'; i++ {
			if s[i] == ')' {
				return append([]byte("$"), s[:i+1]...), i + 1, nil
			}
		}
		return []byte("$"), 0, nil
	case s[0] == '\'':
		return p.ansiCString(characterStart, s)
	case isShellSpecialVar(s[0]):
		return []byte(""), 1, nil
	default:
		return p.resolveName(characterStart, s, lookupEnv)
	}
}

func (p *parser) expandBracedRef(characterStart int, s []byte, lookupEnv LookupEnvFunc) ([]byte, int, error) {
	end := matchBrace(s)
	if end < 0 {
		return nil, 0, p.newParserError(characterStart+1, "unexpected EOF while looking for matching '}'")
	}

	val, err := p.expandBraced(characterStart+1, s[1:end], lookupEnv)
	if err != nil {
		return nil, 0, err
	}

	return val, end + 1, nil
}

func (p *parser) resolveName(characterStart int, s []byte, lookupEnv LookupEnvFunc) ([]byte, int, error) {
	var i int
	if isAlpha(s[0]) || s[0] == '_' {
		for i = 1; i < len(s) && isAlphaNum(s[i]); i++ {
		}
	}
	if i == 0 {
		return []byte("$"), 0, nil
	}

	value, envSet := lookupEnv(s[:i])
	if !envSet && p.cfg.unboundErr {
		return nil, 0, p.newUnboundVariable(characterStart, string(s[:i]))
	}

	return value, i, nil
}

// matchBrace returns the index of the '}' closing the '{' at s[0], or -1 if the
// brace is never closed. Nested `${...}` are taken into account.
func matchBrace(s []byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func (p *parser) expandBraced(characterStart int, inner []byte, lookupEnv LookupEnvFunc) (value []byte, err error) {
	if len(inner) == 0 {
		return nil, p.newParserError(characterStart, "bad substitution: empty")
	}

	if inner[0] == '#' && len(inner) > 1 {
		v, _ := lookupEnv(inner[1:])
		return []byte(strconv.Itoa(len(v))), nil
	}

	var i int
	if isAlpha(inner[0]) || inner[0] == '_' {
		for i = 1; i < len(inner) && isAlphaNum(inner[i]); i++ {
		}
	}
	if i == 0 {
		return nil, p.newParserError(characterStart, "bad substitution")
	}

	name := inner[:i]
	value, envSet := lookupEnv(name)
	rest := inner[i:]

	if len(rest) == 0 {
		if !envSet && p.cfg.unboundErr {
			return nil, p.newUnboundVariable(characterStart, string(name))
		}
		return value, nil
	}

	switch rest[0] {
	case ':':
		if len(rest) == 1 {
			return nil, p.newParserError(characterStart+i, "bad substitution: no modifier")
		}

		switch rest[1] {
		case '-':
			if !envSet || len(value) == 0 {
				return p.expandWord(characterStart+i+2, rest[2:], lookupEnv)
			}
			return value, nil
		case '+':
			if len(value) > 0 {
				return p.expandWord(characterStart+i+2, rest[2:], lookupEnv)
			}
			return nil, nil
		case '?':
			if !envSet || len(value) == 0 {
				return nil, p.newParameterError(characterStart, name, i+2, rest[2:], lookupEnv)
			}
			return value, nil
		case '=':
			return nil, p.newParserError(characterStart+i, "bad substitution: assignment is not supported")
		default:
			return p.substring(characterStart+i, value, rest[1:])
		}
	case '-':
		if !envSet {
			return p.expandWord(characterStart+i+1, rest[1:], lookupEnv)
		}
		return value, nil
	case '+':
		if envSet {
			return p.expandWord(characterStart+i+1, rest[1:], lookupEnv)
		}
		return nil, nil
	case '?':
		if !envSet {
			return nil, p.newParameterError(characterStart, name, i+1, rest[1:], lookupEnv)
		}
		return value, nil
	case '=':
		return nil, p.newParserError(characterStart+i, "bad substitution: assignment is not supported")
	case '#':
		return p.stripPrefix(value, rest)
	case '%':
		return p.stripSuffix(value, rest)
	case '/':
		return p.replace(characterStart+i, value, rest, lookupEnv)
	default:
		return nil, p.newParserError(characterStart+i, "bad substitution: unsupported operator")
	}
}

// expandWord re-scans the word of a parameter expansion for nested $ expansions.
// expandString resolves $NAME and ${...} references in s. It backs Expand.
func (p *parser) expandString(s string, lookupEnv LookupEnvFunc) (string, error) {
	data := []byte(s)
	out := make([]byte, 0, len(data))

	for j := 0; j < len(data); j++ {
		if data[j] != '$' {
			out = append(out, data[j])
			continue
		}

		res, skip, err := p.resolveParameter(j, data[j+1:], lookupEnv)
		if err != nil {
			return "", err
		}
		out = append(out, res...)
		j += skip
	}

	return string(out), nil
}

func (p *parser) expandWord(characterStart int, w []byte, lookupEnv LookupEnvFunc) ([]byte, error) {
	out := make([]byte, 0, len(w))
	for j := 0; j < len(w); j++ {
		switch w[j] {
		case '\\':
			if j+1 < len(w) {
				j++
				out = append(out, w[j])
			}
		case '$':
			res, skip, err := p.resolveParameter(characterStart+j, w[j+1:], lookupEnv)
			if err != nil {
				return nil, err
			}
			out = append(out, res...)
			j += skip
		default:
			out = append(out, w[j])
		}
	}
	return out, nil
}

func (p *parser) newParameterError(characterStart int, name []byte, wordOffset int, word []byte, lookupEnv LookupEnvFunc) error {
	msg, err := p.expandWord(characterStart+wordOffset, word, lookupEnv)
	if err != nil {
		return err
	}
	if len(msg) == 0 {
		return p.newParserError(characterStart, fmt.Sprintf("%s: parameter not set", name))
	}
	return p.newParserError(characterStart, fmt.Sprintf("%s: %s", name, msg))
}

func (p *parser) stripPrefix(value, rest []byte) ([]byte, error) {
	pattern := rest[1:]
	longest := false
	if len(pattern) > 0 && pattern[0] == '#' {
		longest = true
		pattern = pattern[1:]
	}

	if longest {
		for k := len(value); k >= 0; k-- {
			if matchGlob(pattern, value[:k]) {
				return value[k:], nil
			}
		}
	} else {
		for k := 0; k <= len(value); k++ {
			if matchGlob(pattern, value[:k]) {
				return value[k:], nil
			}
		}
	}

	return value, nil
}

func (p *parser) stripSuffix(value, rest []byte) ([]byte, error) {
	pattern := rest[1:]
	longest := false
	if len(pattern) > 0 && pattern[0] == '%' {
		longest = true
		pattern = pattern[1:]
	}

	if longest {
		for k := len(value); k >= 0; k-- {
			if matchGlob(pattern, value[len(value)-k:]) {
				return value[:len(value)-k], nil
			}
		}
	} else {
		for k := 0; k <= len(value); k++ {
			if matchGlob(pattern, value[len(value)-k:]) {
				return value[:len(value)-k], nil
			}
		}
	}

	return value, nil
}

func (p *parser) replace(characterStart int, value, rest []byte, lookupEnv LookupEnvFunc) ([]byte, error) {
	spec := rest[1:]
	all := false
	if len(spec) > 0 && spec[0] == '/' {
		all = true
		spec = spec[1:]
	}

	var anchor byte
	if len(spec) > 0 && (spec[0] == '#' || spec[0] == '%') {
		anchor = spec[0]
		spec = spec[1:]
	}

	pattern, replacement := splitReplacement(spec)

	replacement, err := p.expandWord(characterStart, replacement, lookupEnv)
	if err != nil {
		return nil, err
	}

	out := make([]byte, 0, len(value))
	offset := 0
	for offset <= len(value) {
		start, end, ok := findMatch(pattern, value[offset:], anchor)
		if !ok {
			break
		}

		out = append(out, value[offset:offset+start]...)
		out = append(out, replacement...)
		offset += end

		if !all {
			break
		}
		if end == start {
			if offset < len(value) {
				out = append(out, value[offset])
				offset++
			} else {
				break
			}
		}
	}
	out = append(out, value[offset:]...)

	return out, nil
}

func (p *parser) substring(characterStart int, value, spec []byte) ([]byte, error) {
	offset, rest, ok := parseIndex(spec)
	if !ok {
		return nil, p.newParserError(characterStart, "bad substitution: invalid substring")
	}

	length := len(value)
	begin := offset
	if begin < 0 {
		begin = length + begin
		if begin < 0 {
			return nil, nil
		}
	}
	if begin > length {
		return nil, nil
	}

	rest = bytes.TrimLeft(rest, " ")
	if len(rest) == 0 {
		return value[begin:], nil
	}
	if rest[0] != ':' {
		return nil, p.newParserError(characterStart, "bad substitution: invalid substring")
	}

	end, _, ok := parseIndex(rest[1:])
	if !ok {
		return nil, p.newParserError(characterStart, "bad substitution: invalid substring length")
	}
	if end >= 0 {
		end += begin
		if end > length {
			end = length
		}
	} else {
		end += length
		if end < begin {
			end = begin
		}
	}

	return value[begin:end], nil
}

func parseIndex(spec []byte) (value int, rest []byte, ok bool) {
	i := 0
	for i < len(spec) && spec[i] == ' ' {
		i++
	}

	negative := false
	if i < len(spec) && (spec[i] == '-' || spec[i] == '+') {
		negative = spec[i] == '-'
		i++
	}

	j := i
	for j < len(spec) && isNum(spec[j]) {
		j++
	}
	if j == i {
		return 0, nil, false
	}

	n := 0
	for k := i; k < j; k++ {
		n = n*10 + int(spec[k]-'0')
	}
	if negative {
		n = -n
	}

	return n, spec[j:], true
}

func splitReplacement(spec []byte) (pattern, replacement []byte) {
	for i := 0; i < len(spec); i++ {
		if spec[i] == '\\' {
			i++
			continue
		}
		if spec[i] == '/' {
			return spec[:i], spec[i+1:]
		}
	}
	return spec, nil
}

func findMatch(pattern, str []byte, anchor byte) (start, end int, ok bool) {
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
			pi++
			si++
		case pi < len(pattern) && pattern[pi] == '[':
			next, matched := matchClass(pattern, pi, s[si])
			switch {
			case matched:
				pi = next
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

func matchClass(pattern []byte, start int, c byte) (int, bool) {
	i := start + 1
	negate := false
	if i < len(pattern) && (pattern[i] == '^' || pattern[i] == '!') {
		negate = true
		i++
	}

	matched := false
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
		return len(pattern), false
	}

	return i + 1, matched != negate
}

// ansiCString decodes a bash `$'...'` ANSI-C quoted string beginning at s[0].
func (p *parser) ansiCString(characterStart int, s []byte) ([]byte, int, error) {
	out := make([]byte, 0, len(s))

	for i := 1; i < len(s); {
		c := s[i]
		switch {
		case c == '\'':
			return out, i + 1, nil
		case c != '\\':
			out = append(out, c)
			i++
		case i+1 >= len(s):
			return nil, 0, p.newParserError(characterStart+i, "incomplete escape sequence")
		default:
			decoded, width, err := decodeEscape(s, i+1)
			if err != nil {
				return nil, 0, err
			}
			out = append(out, decoded...)
			i += 1 + width
		}
	}

	return nil, 0, p.newParserError(characterStart, "unmatched single quote")
}

// decodeEscape decodes the escape body that follows a backslash at s[j].
func decodeEscape(s []byte, j int) ([]byte, int, error) {
	switch s[j] {
	case 'a':
		return []byte{0x07}, 1, nil
	case 'b':
		return []byte{0x08}, 1, nil
	case 'e', 'E':
		return []byte{0x1b}, 1, nil
	case 'f':
		return []byte{0x0c}, 1, nil
	case 'n':
		return []byte{0x0a}, 1, nil
	case 'r':
		return []byte{0x0d}, 1, nil
	case 't':
		return []byte{0x09}, 1, nil
	case 'v':
		return []byte{0x0b}, 1, nil
	case '\\', '\'', '"', '?':
		return []byte{s[j]}, 1, nil
	case 'x':
		return decodeHexEscape(s, j, 2, false)
	case 'u':
		return decodeHexEscape(s, j, 4, true)
	case 'U':
		return decodeHexEscape(s, j, 8, true)
	}

	if s[j] >= '0' && s[j] <= '7' {
		n, k := 0, 0
		for k < 3 && j+k < len(s) && s[j+k] >= '0' && s[j+k] <= '7' {
			n = n*8 + int(s[j+k]-'0')
			k++
		}
		return []byte{byte(n)}, k, nil
	}

	return []byte{'\\', s[j]}, 1, nil
}

func decodeHexEscape(s []byte, j, max int, asRune bool) ([]byte, int, error) {
	n, k := 0, 0
	for k < max && j+1+k < len(s) && isHex(s[j+1+k]) {
		n = n*16 + hexVal(s[j+1+k])
		k++
	}
	if k == 0 {
		return []byte{'\\', s[j]}, 1, nil
	}
	if asRune {
		return []byte(string(rune(n))), 1 + k, nil
	}
	return []byte{byte(n)}, 1 + k, nil
}

func decodeUnicodeEscape(s []byte, j int) ([]byte, int) {
	n, k := 0, 0
	for k < 4 && j+1+k < len(s) && isHex(s[j+1+k]) {
		n = n*16 + hexVal(s[j+1+k])
		k++
	}
	if k == 0 {
		return []byte{'u'}, 0
	}
	return []byte(string(rune(n))), k
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func hexVal(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}
