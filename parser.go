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
				break ParseLoop
			case '$':
				name, w := getShellName(line[j+1:])
				if len(name) == 0 {
					if w > 0 {
						// Encountered invalid syntax; eat the
						// characters.
					} else {
						// Valid syntax, but $ was not followed by a
						// name. Leave the dollar character untouched.
						value = append(value, c)
					}
				} else {
					value = append(value, mapping(string(name), envMap)...)
				}
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
				name, w := getShellName(line[j+1:])
				if len(name) == 0 {
					if w > 0 {
						// Encountered invalid syntax; eat the
						// characters.
					} else {
						// Valid syntax, but $ was not followed by a
						// name. Leave the dollar character untouched.
						value = append(value, c)
					}
				} else {
					value = append(value, mapping(string(name), envMap)...)
				}
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

// getShellName returns the name that begins the string and the number of bytes
// consumed to extract it. If the name is enclosed in {}, it's part of a ${}
// expansion and two more bytes are needed than the length of the name.
func getShellName(s []byte) ([]byte, int) {
	switch {
	case s[0] == '{':
		if len(s) > 2 && isShellSpecialVar(s[1]) && s[2] == '}' {
			return s[1:2], 3
		}
		// Scan to closing brace
		for i := 1; i < len(s); i++ {
			if s[i] == '}' {
				if i == 1 {
					return nil, 2 // Bad syntax; eat "${}"
				}
				return s[1:i], i + 1
			}
		}
		return nil, 1 // Bad syntax; eat "${"
	case isShellSpecialVar(s[0]):
		return s[0:1], 1
	}
	// Scan alphanumerics.
	var i int
	for i = 0; i < len(s) && isAlphaNum(s[i]); i++ {
	}
	return s[:i], i
}

func mapping(s string, envMap map[string]string) []byte {
	if v, ok := envMap[s]; ok {
		return []byte(v)
	}

	return []byte(os.Getenv(s))
}
