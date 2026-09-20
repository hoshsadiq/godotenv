package godotenv

import "bytes"

// parseAnsiC decodes a bash `$'...'` ANSI-C quoted string, with the cursor at
// the opening quote. bash stores the decoded bytes in a C string, so a decoded
// NUL truncates the value; the scan still continues to the closing quote.
func (p *parser) parseAnsiC(c *cursor) ([]byte, error) {
	quote := c.pos
	out := make([]byte, 0, 16)
	c.advance(1)

	truncated := false

	for !c.eof() {
		switch ch := c.peek(); {
		case ch == '\'':
			c.advance(1)
			return out, nil
		case ch != '\\':
			if !truncated {
				out = append(out, ch)
			}
			c.advance(1)
		case c.pos+1 >= len(c.data):
			return nil, p.newParserError(c.pos, "incomplete escape sequence")
		default:
			decoded, width, err := decodeEscape(c.data, c.pos+1)
			if err != nil {
				return nil, err
			}
			if !truncated {
				if n := bytes.IndexByte(decoded, 0); n >= 0 {
					out = append(out, decoded[:n]...)
					truncated = true
				} else {
					out = append(out, decoded...)
				}
			}
			c.advance(1 + width)
		}
	}

	return nil, p.newParserError(quote, "unmatched single quote")
}

// expandBashQuote handles the bash-only $'...' and $"..." forms, with the
// cursor at the opening quote. $'...' is ANSI-C decoded; $"..." produces
// nothing because the following "..." is then parsed as a normal quoted
// segment.
func (p *parser) expandBashQuote(c *cursor) ([]byte, error) {
	if c.peek() == '\'' {
		return p.parseAnsiC(c)
	}

	return nil, nil
}

// parsePatternBashDollar handles a bash-only $'...' or $"..." inside a pattern
// operand, escaping the bytes it produces so they match literally. handled is
// false when the byte after the '$' is not a bash quote, leaving the cursor
// unmoved.
func (p *parser) parsePatternBashDollar(c *cursor, out *[]byte, brace int, lookupEnv LookupEnvFunc) (handled bool, err error) {
	switch c.peekAt(1) {
	case '\'':
		c.advance(1)
		decoded, err := p.parseAnsiC(c)
		if err != nil {
			return true, err
		}
		for _, b := range decoded {
			*out = appendPatternByte(*out, b, true)
		}
		return true, nil
	case '"':
		c.advance(1)
		return true, p.parsePatternDoubleQuoted(c, out, brace, lookupEnv)
	}

	return false, nil
}

// decodeBashDoubleQuoteEscape decodes the dotenv-style backslash escape at the
// cursor, with the cursor at the byte after the backslash. A backslash-newline
// is a line continuation and unknown escapes drop the backslash.
func (p *parser) decodeBashDoubleQuoteEscape(c *cursor) ([]byte, error) {
	ch := c.peek()

	if ch == '\r' && c.peekAt(1) == '\n' {
		c.advance(2)
		return nil, nil
	}
	if ch == '\n' {
		c.advance(1)
		return nil, nil
	}

	switch ch {
	case 'a':
		c.advance(1)
		return []byte{0x07}, nil
	case 'b':
		c.advance(1)
		return []byte{'\b'}, nil
	case 'f':
		c.advance(1)
		return []byte{'\f'}, nil
	case 'r':
		c.advance(1)
		return []byte{'\r'}, nil
	case 'n':
		c.advance(1)
		return []byte{'\n'}, nil
	case 't':
		c.advance(1)
		return []byte{'\t'}, nil
	case 'v':
		c.advance(1)
		return []byte{0x0b}, nil
	case 'u':
		decoded, width := decodeUnicodeEscape(p.data, c.pos)
		c.advance(1 + width)
		return decoded, nil
	case 'x':
		decoded, width, err := decodeHexEscape(p.data, c.pos, 2, false)
		if err != nil {
			return nil, err
		}
		c.advance(width)
		return decoded, nil
	case 'U':
		decoded, width, err := decodeHexEscape(p.data, c.pos, 8, true)
		if err != nil {
			return nil, err
		}
		c.advance(width)
		return decoded, nil
	default:
		c.advance(1)
		return []byte{ch}, nil
	}
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
		if j+1 < len(s) && s[j+1] == '{' {
			return decodeBracedHexEscape(s, j)
		}
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

// decodeBracedHexEscape decodes bash 5.3's `\x{...}` form, with j at the 'x'.
// bash consumes every hex digit after the brace, skips a closing '}' when one
// follows, and keeps only the low byte of the accumulated value. The returned
// width is measured from j, matching decodeEscape's contract.
func decodeBracedHexEscape(s []byte, j int) ([]byte, int, error) {
	i := j + 2 // past "\x{"
	n := 0
	for i < len(s) && isHex(s[i]) {
		n = n*16 + hexVal(s[i])
		i++
	}
	if i < len(s) && s[i] == '}' {
		i++
	}

	return []byte{byte(n)}, i - j, nil
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
		return []byte{'\\', 'u'}, 0
	}
	return []byte(string(rune(n))), k
}
