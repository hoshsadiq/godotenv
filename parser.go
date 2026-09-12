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

// lookupEnvFunc is used to determine the value of an environment, and whether it exists or not.
// This should only look at the application environment and not at previous parsed items in a .env file.
// Previously parsed items in an .env file take precedence over the environment.
type lookupEnvFunc func(name []byte) (value []byte, exists bool)

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

func (p *parser) parse(m map[string]string, lookupEnv lookupEnvFunc) (err error) {
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
				state = stateEscapeDouble
			case '\n':
				p.lineNumber++
				fallthrough
			default:
				value = append(value, c)
			}
		case stateEscapeDouble:
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
				panic("todo parse: \\u")
			default:
				value = append(value, c)
			}

			state = stateQuoteDouble
		case stateQuoteSingle:
			switch c {
			case '\'':
				state = stateValue
			case '\\':
				state = stateEscapeSingle
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
func (p *parser) resolveParameter(characterStart int, s []byte, lookupEnv lookupEnvFunc) (res []byte, skip int, err error) {
	if len(s) == 0 {
		return []byte("$"), 0, nil
	}

	switch {
	case s[0] == '{':
		end := matchBrace(s)
		if end < 0 {
			return nil, 0, p.newParserError(characterStart+1, "unexpected EOF while looking for matching '}'")
		}

		val, err := p.expandBraced(characterStart+1, s[1:end], lookupEnv)
		if err != nil {
			return nil, 0, err
		}

		return val, end + 1, nil
	case s[0] == '(':
		// Command substitution is never executed. Preserve `$(...)` verbatim
		for i := 1; i < len(s) && s[i] != '\n'; i++ {
			if s[i] == ')' {
				return append([]byte("$"), s[:i+1]...), i + 1, nil
			}
		}
		return []byte("$"), 0, nil
	case isShellSpecialVar(s[0]):
		return []byte(""), 1, nil
	default:
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

func (p *parser) expandBraced(characterStart int, inner []byte, lookupEnv lookupEnvFunc) (value []byte, err error) {
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
			return nil, p.newParserError(characterStart+i, "bad substitution: unsupported operator")
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
	default:
		return nil, p.newParserError(characterStart+i, "bad substitution: unsupported operator")
	}
}

// expandWord re-scans the word of a parameter expansion for nested $ expansions.
func (p *parser) expandWord(characterStart int, w []byte, lookupEnv lookupEnvFunc) ([]byte, error) {
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

func (p *parser) newParameterError(characterStart int, name []byte, wordOffset int, word []byte, lookupEnv lookupEnvFunc) error {
	msg, err := p.expandWord(characterStart+wordOffset, word, lookupEnv)
	if err != nil {
		return err
	}
	if len(msg) == 0 {
		return p.newParserError(characterStart, fmt.Sprintf("%s: parameter not set", name))
	}
	return p.newParserError(characterStart, fmt.Sprintf("%s: %s", name, msg))
}
