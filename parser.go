package godotenv

import (
	"bytes"
	"fmt"
	"os"
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

func parseLine(lineNumber int, line []byte, envMap map[string]string) (key []byte, value []byte, err error) {
	if len(line) == 0 || line[0] == '#' {
		return
	}

	equals := bytes.IndexByte(line, '=')
	if equals == -1 {
		return nil, nil, newParserError(line, lineNumber, 0, "invalid environment line")
	}

	key = make([]byte, 0, equals)
	value = make([]byte, 0, len(line)-1-equals)

	state := stateKey

ParseLoop:
	for j := 0; j < len(line); j++ {
		c := line[j]

		switch state {
		case stateKey:
			switch {
			case c == '=':
				if len(key) == 0 {
					return nil, nil, newParserError(line, lineNumber, j, "empty key")
				}

				state = stateValue
			case c == '#':
				break ParseLoop
			case c == ' ', c == '\t':
				if bytes.Equal(key, []byte(exportPrefix)) {
					key = key[:0]
				}

				// ignore empty space
				if len(key) == 0 {
					continue
				}

				return nil, nil, newParserError(line, lineNumber, j, "unexpected space in key")
			case unicode.IsNumber(rune(c)):
				if len(key) == 0 {
					return nil, nil, newParserError(line, lineNumber, j, "invalid character in key name")
				}
				fallthrough
			case c == '_':
				fallthrough
			case unicode.IsLetter(rune(c)):
				key = append(key, c)
			default:
				return nil, nil, newParserError(line, lineNumber, j, "invalid character in key name")
			}
		case stateValue:
			switch c {
			case '\\':
				state = stateEscapeNone
			case '\'':
				state = stateQuoteSingle
			case '"':
				state = stateQuoteDouble
			case '#':
				if unicode.IsSpace(rune(line[j-1])) {
					break ParseLoop
				}

				value = append(value, c)
			case '$':
				res, w, err := resolveParameter(line, lineNumber, j, line[j+1:], envMap)
				if err != nil {
					return nil, nil, err
				}
				value = append(value, res...)
				j += w
			case ' ':
				if len(value) == 0 {
					return nil, nil, newParserError(line, lineNumber, j, "unexpected space in value")
				}
			default:
				if c < 32 {
					return nil, nil, newInvalidCharacterError(line, lineNumber, j, c)

				}
				value = append(value, c)
			}
		case stateEscapeNone:
			value = append(value, c)
			state = stateValue
		case stateQuoteDouble:
			switch c {
			case '$':
				res, w, err := resolveParameter(line, lineNumber, j, line[j+1:], envMap)
				if err != nil {
					return nil, nil, err
				}
				value = append(value, res...)
				j += w
			case '"':
				state = stateValue
			case '\\':
				state = stateEscapeDouble
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

	switch state {
	case stateValue:
	case stateKey:
		return nil, nil, newParserError(line, lineNumber, len(line), "missing value operator")
	case stateQuoteDouble:
		return nil, nil, newParserError(line, lineNumber, len(line), "unmatched double quote")
	case stateQuoteSingle:
		return nil, nil, newParserError(line, lineNumber, len(line), "unmatched single quote")
	case stateEscapeNone, stateEscapeDouble, stateEscapeSingle: // todo this can be resolved by dealing with the whole input instead of line by line
		return nil, nil, newParserError(line, lineNumber, len(line), "incomplete escape sequence")
	default:
		panic(fmt.Errorf("state is invalid: %v. THIS IS A BUG", state))
	}

	return key, value, nil
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

// isAlphaNum reports whether the byte is an ASCII letter, number, or underscore
func isAlphaNum(c uint8) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
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
func resolveParameter(line []byte, lineNumber int, characterStart int, s []byte, envMap map[string]string) (name []byte, skip int, err error) {
	switch {
	case s[0] == '{':
		// Scan to closing brace
		for i := 1; i < len(s); i++ {
			if s[i] == '}' {
				if i == 1 {
					return nil, 2, nil // Bad syntax; eat "${}"
				}
				val, err := expandParameter(line, lineNumber, characterStart+1, s[1:i], envMap)
				return val, i + 1, err
			}
		}
		return nil, 0, newParserError(line, lineNumber, characterStart+1, "unmatched parenthesis for parameter")
	case isShellSpecialVar(s[0]):
		parameter, err := expandParameter(line, lineNumber, characterStart, s[0:1], envMap)
		return parameter, 1, err
	default:
		// Scan alphanumerics.
		var i int
		for i = 0; i < len(s) && isAlphaNum(s[i]); i++ {
		}
		parameter, err := expandParameter(line, lineNumber, characterStart, s[:i], envMap)
		return parameter, i, err
	}
}

func expandParameter(line []byte, lineNumber, characterStart int, s []byte, envMap map[string]string) (value []byte, err error) {
	var varSet bool
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ':':
			if i == len(s) {
				return nil, newParserError(line, lineNumber, characterStart, "unrecognized modifier")
			}

			value, varSet = expandEnvMap(s[0:i], envMap)
			switch s[i+1] {
			case '-':
				if !varSet || len(value) == 0 {
					return s[i+2:], nil
				}

				return value, nil
			case '+':
				if len(value) > 0 {
					return s[i+2:], nil
				}

				return value, nil
			default:
				return nil, newParserError(line, lineNumber, characterStart+i+1, "unrecognized modifier")
			}
		case '-':
			value, varSet = expandEnvMap(s[0:i], envMap)
			if !varSet {
				return s[i+1:], nil
			}

			return value, nil
		case '+':
			value, varSet = expandEnvMap(s[0:i], envMap)
			if varSet {
				return s[i+1:], nil
			}

			return value, nil
		}
	}

	value, _ = expandEnvMap(s, envMap)
	return
}

func expandEnvMap(s []byte, envMap map[string]string) (value []byte, exists bool) {
	var val string

	if val, exists = envMap[string(s)]; exists {
		return []byte(val), exists
	}

	val, exists = os.LookupEnv(string(s))
	return []byte(val), exists
}
