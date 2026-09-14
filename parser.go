package godotenv

import (
	"bytes"
	"unicode/utf8"
)

const (
	exportPrefix      = "export"
	maxExpansionDepth = 1000
)

// LookupEnvFunc is used to determine the value of an environment, and whether it exists or not.
// This should only look at the application environment and not at previous parsed items in a .env file.
// Previously parsed items in an .env file take precedence over the environment.
type LookupEnvFunc func(name []byte) (value []byte, exists bool)

type parser struct {
	data      []byte
	cfg       config
	depth     int
	value     []byte
	pendingWS []byte
}

func newParser(d []byte, cfg config) *parser {
	return &parser{
		data: d,
		cfg:  cfg,
	}
}

type cursor struct {
	data []byte
	pos  int
}

func (c *cursor) eof() bool {
	return c.pos >= len(c.data)
}

func (c *cursor) peek() byte {
	if c.eof() {
		return 0
	}
	return c.data[c.pos]
}

func (c *cursor) peekAt(offset int) byte {
	if c.pos+offset >= len(c.data) {
		return 0
	}
	return c.data[c.pos+offset]
}

func (c *cursor) advance(n int) {
	c.pos += n
	if c.pos > len(c.data) {
		c.pos = len(c.data)
	}
}

func (p *parser) parse(m map[string]string, lookupEnv LookupEnvFunc) error {
	lineCap := longestLine(p.data)

	key := make([]byte, 0, lineCap)
	p.value = make([]byte, 0, lineCap)
	p.pendingWS = make([]byte, 0, lineCap)

	c := &cursor{data: p.data}

	for !c.eof() {
		p.skipBlankAndComments(c)
		if c.eof() {
			break
		}

		var err error
		if key, err = p.parseKey(c, key[:0]); err != nil {
			return err
		}
		if len(key) == 0 {
			continue
		}

		if c.eof() || c.peek() != '=' {
			return p.newParserError(c.pos, "missing value operator")
		}
		c.advance(1)

		if err = p.parseValue(c, lookupEnv); err != nil {
			return err
		}

		m[string(key)] = string(p.value)
	}

	return nil
}

func (p *parser) skipBlankAndComments(c *cursor) {
	for !c.eof() {
		switch c.peek() {
		case ' ', '\t', '\r', '\n':
			c.advance(1)
		case '#':
			for !c.eof() && c.peek() != '\n' {
				c.advance(1)
			}
		default:
			return
		}
	}
}

func (p *parser) parseKey(c *cursor, key []byte) ([]byte, error) {
	switch ch := c.peek(); {
	case ch == '=':
		return nil, p.newParserError(c.pos, "empty key")
	case !isAlpha(ch) && ch != '_':
		return nil, p.newParserError(c.pos, "invalid character in key name")
	}

	for !c.eof() && isAlphaNum(c.peek()) {
		key = append(key, c.peek())
		c.advance(1)
	}

	if c.eof() {
		return key, nil
	}

	switch ch := c.peek(); ch {
	case '=':
		return key, nil
	case ' ', '\t', '\r', '\n':
		if bytes.Equal(key, []byte(exportPrefix)) {
			return key[:0], nil
		}
		return nil, p.newParserError(c.pos, "unexpected whitespace in key")
	case '#':
		return nil, p.newParserError(c.pos, "not a valid identifier")
	default:
		return nil, p.newParserError(c.pos, "invalid character in key name")
	}
}

func (p *parser) parseValue(c *cursor, lookupEnv LookupEnvFunc) error {
	p.value = p.value[:0]
	p.pendingWS = p.pendingWS[:0]

	started := false
	startValue := func(pos int) error {
		if !started && len(p.pendingWS) > 0 {
			return p.newParserError(pos-len(p.pendingWS), "unexpected space in value")
		}

		started = true

		return nil
	}

	for !c.eof() {
		switch ch := c.peek(); ch {
		case '\r':
			// ignore `\r` in an `\r\n`, but not in only `\r`
			if c.peekAt(1) == '\n' {
				c.advance(1)
				continue
			}

			if err := startValue(c.pos); err != nil {
				return err
			}

			p.flushPending()
			p.value = append(p.value, ch)
			c.advance(1)
		case '\n':
			c.advance(1)
			return nil
		case '\\':
			if err := startValue(c.pos); err != nil {
				return err
			}

			p.flushPending()
			c.advance(1)
			if c.eof() {
				return p.newParserError(c.pos, "incomplete escape sequence")
			}
			if c.peek() == '\r' && c.peekAt(1) == '\n' {
				c.advance(2)
				continue
			}
			if c.peek() == '\n' {
				c.advance(1)
				continue
			}

			p.value = append(p.value, c.peek())
			c.advance(1)
		case '\'':
			if err := startValue(c.pos); err != nil {
				return err
			}

			p.flushPending()
			if err := p.parseSingleQuoted(c); err != nil {
				return err
			}
		case '"':
			if err := startValue(c.pos); err != nil {
				return err
			}

			p.flushPending()
			if err := p.parseDoubleQuoted(c, lookupEnv); err != nil {
				return err
			}
		case '#':
			if isSpace(c.data[c.pos-1]) {
				for !c.eof() && c.peek() != '\n' {
					c.advance(1)
				}
				continue
			}

			if err := startValue(c.pos); err != nil {
				return err
			}

			p.value = append(p.value, ch)
			c.advance(1)
		case '$':
			if err := startValue(c.pos); err != nil {
				return err
			}

			p.flushPending()
			res, err := p.expandDollar(c, true, lookupEnv)
			if err != nil {
				return err
			}

			p.value = append(p.value, res...)
		case ' ', '\t':
			p.pendingWS = append(p.pendingWS, ch)
			c.advance(1)
		default:
			if ch < 32 {
				return p.newInvalidCharacterError(c.pos, ch)
			}

			if err := startValue(c.pos); err != nil {
				return err
			}

			p.flushPending()
			start := c.pos
			for !c.eof() && isPlainValueByte(c.peek()) {
				c.advance(1)
			}
			p.value = append(p.value, c.data[start:c.pos]...)
		}
	}

	return nil
}

func (p *parser) flushPending() {
	p.value = append(p.value, p.pendingWS...)
	p.pendingWS = p.pendingWS[:0]
}

func (p *parser) parseSingleQuoted(c *cursor) error {
	c.advance(1)

	for !c.eof() {
		switch ch := c.peek(); ch {
		case '\'':
			c.advance(1)
			return nil
		case '\\':
			p.value = append(p.value, '\\')
			c.advance(1)
			if p.cfg.posix {
				continue
			}
			if c.eof() {
				return p.newParserError(c.pos, "incomplete escape sequence")
			}

			p.value = append(p.value, c.peek())
			c.advance(1)
		default:
			p.value = append(p.value, ch)
			c.advance(1)
		}
	}

	return p.newParserError(c.pos, "unmatched single quote")
}

func (p *parser) parseDoubleQuoted(c *cursor, lookupEnv LookupEnvFunc) error {
	c.advance(1)

	for !c.eof() {
		switch ch := c.peek(); ch {
		case '"':
			c.advance(1)
			return nil
		case '$':
			res, err := p.expandDollar(c, false, lookupEnv)
			if err != nil {
				return err
			}

			p.value = append(p.value, res...)
		case '\\':
			if !p.cfg.posix {
				if err := p.parseDoubleQuoteEscape(c); err != nil {
					return err
				}
				continue
			}

			if c.pos+1 >= len(c.data) {
				return p.newParserError(c.pos, "incomplete escape sequence")
			}

			c.advance(1)
			switch next := c.peek(); next {
			case '$', '`', '"', '\\':
				p.value = append(p.value, next)
				c.advance(1)
			case '\n':
				c.advance(1)
			default:
				p.value = append(p.value, '\\')
			}
		default:
			p.value = append(p.value, ch)
			c.advance(1)
		}
	}

	return p.newParserError(c.pos, "unmatched double quote")
}

func (p *parser) parseDoubleQuoteEscape(c *cursor) error {
	c.advance(1)

	if c.eof() {
		return p.newParserError(c.pos, "incomplete escape sequence")
	}

	ch := c.peek()
	if ch == '\r' && c.peekAt(1) == '\n' {
		c.advance(2)
		return nil
	}
	if ch == '\n' {
		c.advance(1)
		return nil
	}

	switch ch {
	case 'b':
		p.value = append(p.value, '\b')
		c.advance(1)
	case 'f':
		p.value = append(p.value, '\f')
		c.advance(1)
	case 'r':
		p.value = append(p.value, '\r')
		c.advance(1)
	case 'n':
		p.value = append(p.value, '\n')
		c.advance(1)
	case 't':
		p.value = append(p.value, '\t')
		c.advance(1)
	case 'u':
		decoded, width := decodeUnicodeEscape(p.data, c.pos)
		p.value = append(p.value, decoded...)
		c.advance(1 + width)
	case 'x':
		decoded, width, err := decodeHexEscape(p.data, c.pos, 2, false)
		if err != nil {
			return err
		}
		p.value = append(p.value, decoded...)
		c.advance(width)
	case 'U':
		decoded, width, err := decodeHexEscape(p.data, c.pos, 8, true)
		if err != nil {
			return err
		}
		p.value = append(p.value, decoded...)
		c.advance(width)
	default:
		p.value = append(p.value, ch)
		c.advance(1)
	}

	return nil
}

// longestLine returns the length of the longest line in data, without its
// newline. It sizes the parse buffers, which only need to hold one line at a
// time.
func longestLine(data []byte) int {
	longest, start := 0, 0
	for i, c := range data {
		if c != '\n' {
			continue
		}
		if i-start > longest {
			longest = i - start
		}
		start = i + 1
	}
	if len(data)-start > longest {
		longest = len(data) - start
	}
	return longest
}

// runeBoundaries returns the byte offsets at which runes start, plus len(s).
func runeBoundaries(s []byte) []int {
	bounds := make([]int, 0, len(s)+1)
	for i := 0; i < len(s); {
		bounds = append(bounds, i)
		_, size := utf8.DecodeRune(s[i:])
		i += size
	}
	return append(bounds, len(s))
}

// byteOffset returns the byte offset of the n-th rune in value.
func byteOffset(value []byte, n int) int {
	i := 0
	for ; n > 0 && i < len(value); n-- {
		_, size := utf8.DecodeRune(value[i:])
		i += size
	}
	return i
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isPlainValueByte(c byte) bool {
	switch c {
	case '\\', '\'', '"', '#', '$', ' ', '\t', '\r', '\n':
		return false
	}

	return c >= 32
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
