package godotenv

import (
	"bytes"
	"fmt"
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
}

func newParser(d []byte) *parser {
	return &parser{
		data:       d,
		lineNumber: 1,
	}
}

func (p *parser) parse(m map[string]string, lookupEnv lookupEnvFunc) (err error) {
	key := make([]byte, 0, len(p.data))
	value := make([]byte, 0, len(p.data))

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
				if unicode.IsSpace(rune(p.data[j-1])) {
					j += bytes.IndexByte(p.data[j+1:], '\n')
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
			case '\n':
				p.lineNumber++

				m[string(key)] = string(value)
				key = key[:0]
				value = value[:0]
				state = stateKey
			case '\\':
				state = stateEscapeNone
			case '\'':
				state = stateQuoteSingle
			case '"':
				state = stateQuoteDouble
			case '#':
				if unicode.IsSpace(rune(p.data[j-1])) {
					j += bytes.IndexByte(append(p.data[j+1:], '\n'), '\n')
					continue
				}

				value = append(value, c)
			case '$':
				res, w, err := p.resolveParameter(j, p.data[j+1:], lookupEnv)
				if err != nil {
					return err
				}
				value = append(value, res...)
				j += w
			case ' ':
				if len(value) == 0 {
					return p.newParserError(j, "unexpected space in value")
				}
			default:
				if c < 32 {
					return p.newInvalidCharacterError(j, c)
				}

				value = append(value, c)
			}
		case stateEscapeNone:
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

// resolveParameter resolves the parameter that begins the string and the number of bytes
// consumed to extract the name of it. If the name is enclosed in {}, it's part of a ${}
// expansion and this will be expanded based on a subset of POSIX specification. Namely:
// ${VAR}				No parameter expansion
// ${VAR:-STRING}		If VAR is empty or unset, use STRING as its value.
// ${VAR-STRING}		If VAR is unset, use STRING as its value.
// ${VAR:+STRING}		If VAR is not empty, use STRING as its value.
// ${VAR+STRING}		If VAR is set, use STRING as its value.
// https://steinbaugh.com/posts/posix.html#default-value
// todo we should combine expandParameter with this function to avoid looping over the parameter twice.
func (p *parser) resolveParameter(characterStart int, s []byte, lookupEnv lookupEnvFunc) (name []byte, skip int, err error) {
	switch {
	case s[0] == '{':
		// Scan to closing brace
		i := bytes.IndexByte(s, '}')
		if i != -1 {
			if i == 1 {
				return nil, 2, nil // bad syntax; eat "${}"
			}

			val, err := p.expandParameter(characterStart+1, s[1:i], lookupEnv)
			return val, i + 1, err
		}

		return nil, 0, p.newParserError(characterStart+1, "unexpected EOF while looking for matching '}'")
	case isShellSpecialVar(s[0]):
		// todo how can we expand these things?
		// one idea might be to have a special option that allows
		// one to pass in the relevant arguments and expansion possibilities
		return []byte(""), 1, nil
	default:
		// Scan alphanumerics.
		var i int
		for i = 0; i < len(s) && isAlphaNum(s[i]); i++ {
		}
		parameter, err := p.expandParameter(characterStart, s[:i], lookupEnv)
		return parameter, i, err
	}
}

// todo needs better error messages.
func (p *parser) expandParameter(characterStart int, s []byte, lookupEnv lookupEnvFunc) (value []byte, err error) {
	var envSet bool

	if len(s) > 0 && isNum(s[0]) {
		return nil, p.newParserError(characterStart, "invalid identifier")
	}

	var i int
	for i = 1; i < len(s) && isAlphaNum(s[i]); i++ {
	}

	value, envSet = lookupEnv(s[:i])
	if i >= len(s) {
		// todo error when not set
		// return nil, p.newUnboundVariable(characterStart, s[:i])
		return
	}

	switch s[i] {
	case ':':
		if i == len(s) {
			return nil, p.newParserError(characterStart+i, "bad substitution: no modifier")
		}

		switch s[i+1] {
		case '-':
			if !envSet || len(value) == 0 {
				value = s[i+2:]
			}
		case '+':
			if len(value) > 0 {
				value = s[i+2:]
			}
		default:
			return nil, p.newParserError(characterStart+i+1, "bad substitution: no modifier")
		}
	case '-':
		if !envSet {
			value = s[i+1:]
		}
	case '+':
		if envSet {
			value = s[i+1:]
		}
	}

	return
}
