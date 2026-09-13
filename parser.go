package godotenv

import (
	"bytes"
	"fmt"
	"strconv"
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
			p.value = append(p.value, ch)
			c.advance(1)
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

// expandDollar consumes the parameter starting at the '$' under the cursor and
// returns its expansion. It handles $NAME and the ${NAME<op><word>} forms from
// the POSIX shell parameter expansion rules.
func (p *parser) expandDollar(c *cursor, allowANSI bool, lookupEnv LookupEnvFunc) ([]byte, error) {
	dollar := c.pos
	c.advance(1)

	if c.eof() {
		return []byte("$"), nil
	}

	switch ch := c.peek(); {
	case ch == '{':
		return p.parseBraced(c, lookupEnv)
	case p.cfg.restricted:
		return p.expandName(c, dollar, lookupEnv)
	case ch == '(':
		end, ok := scanCommandSub(c.data, c.pos)
		if !ok {
			return []byte("$"), nil
		}

		res := append([]byte("$"), c.data[c.pos:end]...)
		c.pos = end

		return res, nil
	case allowANSI && ch == '\'':
		return p.parseAnsiC(c)
	case allowANSI && ch == '"':
		return nil, nil
	case isShellSpecialVar(ch):
		c.advance(1)
		return nil, nil
	default:
		return p.expandName(c, dollar, lookupEnv)
	}
}

func (p *parser) expandName(c *cursor, dollar int, lookupEnv LookupEnvFunc) ([]byte, error) {
	if c.eof() || (!isAlpha(c.peek()) && c.peek() != '_') {
		return []byte("$"), nil
	}

	start := c.pos
	for !c.eof() && isAlphaNum(c.peek()) {
		c.advance(1)
	}

	name := c.data[start:c.pos]
	value, envSet := lookupEnv(name)
	if !envSet && p.cfg.unboundErr {
		return nil, p.newUnboundVariable(dollar, string(name))
	}

	return value, nil
}

// scanCommandSub returns the offset just past the ')' closing the '(' at start.
// Command substitution is never executed; it is only preserved verbatim.
func scanCommandSub(data []byte, start int) (end int, ok bool) {
	depth := 0
	var quote byte

	for i := start; i < len(data); i++ {
		switch ch := data[i]; {
		case ch == '\n':
			return 0, false
		case quote == '\'':
			if ch == '\'' {
				quote = 0
			}
		case quote == '"':
			switch ch {
			case '\\':
				i++
			case '"':
				quote = 0
			}
		case ch == '\'' || ch == '"':
			quote = ch
		case ch == '\\':
			i++
		case ch == '(':
			depth++
		case ch == ')':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}

	return 0, false
}

func (p *parser) parseBraced(c *cursor, lookupEnv LookupEnvFunc) ([]byte, error) {
	brace := c.pos
	c.advance(1)

	if c.peek() == '#' {
		return p.parseLength(c, brace, lookupEnv)
	}
	if c.peek() == '}' {
		return nil, p.newParserError(brace, "bad substitution: empty")
	}

	name := p.parseName(c)
	if len(name) == 0 {
		return nil, p.newParserError(brace, "bad substitution")
	}

	nameEnd := c.pos
	value, envSet := lookupEnv(name)

	if c.eof() {
		return nil, p.newParserError(brace, "unexpected EOF while looking for matching '}'")
	}

	colon := false
	if c.peek() == ':' {
		colon = true
		c.advance(1)
		if c.eof() || c.peek() == '}' {
			return nil, p.newParserError(nameEnd, "bad substitution: no modifier")
		}
	}

	opPos := c.pos

	if colon {
		switch op := c.peek(); op {
		case '-':
			c.advance(1)
			if !envSet || len(value) == 0 {
				return p.parseWordUntilBrace(c, brace, lookupEnv)
			}
			if err := p.skipWord(c, brace); err != nil {
				return nil, err
			}
			return value, nil
		case '+':
			c.advance(1)
			if len(value) > 0 {
				return p.parseWordUntilBrace(c, brace, lookupEnv)
			}
			if err := p.skipWord(c, brace); err != nil {
				return nil, err
			}
			return nil, nil
		case '?':
			c.advance(1)
			if !envSet || len(value) == 0 {
				return p.parameterError(c, brace, name, lookupEnv)
			}
			if err := p.skipWord(c, brace); err != nil {
				return nil, err
			}
			return value, nil
		case '=':
			return nil, p.newParserError(opPos, "bad substitution: assignment is not supported")
		default:
			return p.expandSubstring(c, brace, opPos, value)
		}
	}

	switch op := c.peek(); op {
	case '}':
		c.advance(1)
		if !envSet && p.cfg.unboundErr {
			return nil, p.newUnboundVariable(brace, string(name))
		}
		return value, nil
	case '-':
		c.advance(1)
		if !envSet {
			return p.parseWordUntilBrace(c, brace, lookupEnv)
		}
		if err := p.skipWord(c, brace); err != nil {
			return nil, err
		}
		return value, nil
	case '+':
		c.advance(1)
		if envSet {
			return p.parseWordUntilBrace(c, brace, lookupEnv)
		}
		if err := p.skipWord(c, brace); err != nil {
			return nil, err
		}
		return nil, nil
	case '?':
		c.advance(1)
		if !envSet {
			return p.parameterError(c, brace, name, lookupEnv)
		}
		if err := p.skipWord(c, brace); err != nil {
			return nil, err
		}
		return value, nil
	case '=':
		return nil, p.newParserError(opPos, "bad substitution: assignment is not supported")
	case '#', '%':
		rest := scanRaw(c, false)
		if err := p.consumeBrace(c, brace); err != nil {
			return nil, err
		}
		if op == '#' {
			return p.stripPrefix(value, rest)
		}
		return p.stripSuffix(value, rest)
	case '/':
		return p.expandReplace(c, brace, value, lookupEnv)
	default:
		return nil, p.newParserError(opPos, "bad substitution: unsupported operator")
	}
}

func (p *parser) parseLength(c *cursor, brace int, lookupEnv LookupEnvFunc) ([]byte, error) {
	c.advance(1)

	name := p.parseName(c)
	if len(name) == 0 || c.eof() || c.peek() != '}' {
		return nil, p.newParserError(brace, "bad substitution")
	}
	c.advance(1)

	value, envSet := lookupEnv(name)
	if !envSet && p.cfg.unboundErr {
		return nil, p.newUnboundVariable(brace, string(name))
	}

	return []byte(strconv.Itoa(utf8.RuneCount(value))), nil
}

func (p *parser) parseName(c *cursor) []byte {
	if c.eof() || (!isAlpha(c.peek()) && c.peek() != '_') {
		return nil
	}

	start := c.pos
	for !c.eof() && isAlphaNum(c.peek()) {
		c.advance(1)
	}

	return c.data[start:c.pos]
}

func (p *parser) parseWordUntilBrace(c *cursor, brace int, lookupEnv LookupEnvFunc) ([]byte, error) {
	word, err := p.parseWord(c, lookupEnv)
	if err != nil {
		return nil, err
	}
	if err := p.consumeBrace(c, brace); err != nil {
		return nil, err
	}

	return word, nil
}

func (p *parser) skipWord(c *cursor, brace int) error {
	scanRaw(c, false)
	return p.consumeBrace(c, brace)
}

func (p *parser) consumeBrace(c *cursor, brace int) error {
	if c.eof() || c.peek() != '}' {
		return p.newParserError(brace, "unexpected EOF while looking for matching '}'")
	}
	c.advance(1)

	return nil
}

func (p *parser) parameterError(c *cursor, brace int, name []byte, lookupEnv LookupEnvFunc) ([]byte, error) {
	word, err := p.parseWord(c, lookupEnv)
	if err != nil {
		return nil, err
	}
	if err := p.consumeBrace(c, brace); err != nil {
		return nil, err
	}

	if len(word) == 0 {
		return nil, p.newParserError(brace, fmt.Sprintf("%s: parameter not set", name))
	}

	return nil, p.newParserError(brace, fmt.Sprintf("%s: %s", name, word))
}

func (p *parser) expandSubstring(c *cursor, brace, opPos int, value []byte) ([]byte, error) {
	spec := scanRaw(c, false)
	if err := p.consumeBrace(c, brace); err != nil {
		return nil, err
	}

	return p.substring(opPos, value, spec)
}

func (p *parser) expandReplace(c *cursor, brace int, value []byte, lookupEnv LookupEnvFunc) ([]byte, error) {
	c.advance(1)

	all := false
	if c.peek() == '/' {
		all = true
		c.advance(1)
	}

	var anchor byte
	if c.peek() == '#' || c.peek() == '%' {
		anchor = c.peek()
		c.advance(1)
	}

	pattern := scanRaw(c, true)

	var replacement []byte
	if c.peek() == '/' {
		c.advance(1)

		var err error
		if replacement, err = p.parseWord(c, lookupEnv); err != nil {
			return nil, err
		}
	}

	if err := p.consumeBrace(c, brace); err != nil {
		return nil, err
	}

	return p.replace(value, pattern, replacement, all, anchor), nil
}

func scanRaw(c *cursor, stopAtSlash bool) []byte {
	start := c.pos
	depth := 0
	var quote byte

	for !c.eof() {
		switch ch := c.peek(); {
		case ch == '\\':
			c.advance(2)
			continue
		case stopAtSlash && ch == '/':
			return c.data[start:c.pos]
		case quote == '\'':
			if ch == '\'' {
				quote = 0
			}
		case quote == '"':
			if ch == '"' {
				quote = 0
			}
		case ch == '\'' || ch == '"':
			quote = ch
		case ch == '{':
			depth++
		case ch == '}':
			if depth == 0 {
				return c.data[start:c.pos]
			}
			depth--
		}
		c.advance(1)
	}

	return c.data[start:c.pos]
}

func (p *parser) parseWord(c *cursor, lookupEnv LookupEnvFunc) ([]byte, error) {
	if p.depth >= maxExpansionDepth {
		return nil, p.newParserError(c.pos, "expansion nesting too deep")
	}
	p.depth++
	defer func() { p.depth-- }()

	out := make([]byte, 0, 16)
	depth := 0

	for !c.eof() {
		switch ch := c.peek(); ch {
		case '}':
			if depth == 0 {
				return out, nil
			}
			depth--
			out = append(out, ch)
			c.advance(1)
		case '{':
			depth++
			out = append(out, ch)
			c.advance(1)
		case '\\':
			c.advance(1)
			if !c.eof() {
				out = append(out, c.peek())
				c.advance(1)
			}
		case '\'':
			c.advance(1)
			for !c.eof() && c.peek() != '\'' {
				out = append(out, c.peek())
				c.advance(1)
			}
			if !c.eof() {
				c.advance(1)
			}
		case '"':
			c.advance(1)
			for !c.eof() && c.peek() != '"' {
				if c.peek() == '\\' {
					c.advance(1)
					if c.eof() {
						out = append(out, '\\')
						break
					}
					switch c.peek() {
					case 'n':
						out = append(out, '\n')
					case 't':
						out = append(out, '\t')
					default:
						out = append(out, c.peek())
					}
					c.advance(1)
					continue
				}
				if c.peek() == '$' {
					res, err := p.expandDollar(c, false, lookupEnv)
					if err != nil {
						return nil, err
					}
					out = append(out, res...)
					continue
				}
				out = append(out, c.peek())
				c.advance(1)
			}
			if !c.eof() {
				c.advance(1)
			}
		case '$':
			res, err := p.expandDollar(c, true, lookupEnv)
			if err != nil {
				return nil, err
			}
			out = append(out, res...)
		default:
			out = append(out, ch)
			c.advance(1)
		}
	}

	return out, nil
}

// expandString resolves $NAME and ${...} references in s. It backs Expand.
func (p *parser) expandString(s string, lookupEnv LookupEnvFunc) (string, error) {
	data := []byte(s)
	out := make([]byte, 0, len(data))
	c := &cursor{data: data}

	for !c.eof() {
		if c.peek() != '$' {
			out = append(out, c.peek())
			c.advance(1)
			continue
		}

		res, err := p.expandDollar(c, true, lookupEnv)
		if err != nil {
			return "", err
		}
		out = append(out, res...)
	}

	return string(out), nil
}

func (p *parser) stripPrefix(value, rest []byte) ([]byte, error) {
	pattern := rest[1:]
	longest := false
	if len(pattern) > 0 && pattern[0] == '#' {
		longest = true
		pattern = pattern[1:]
	}

	bounds := runeBoundaries(value)
	if longest {
		for i := len(bounds) - 1; i >= 0; i-- {
			if matchGlob(pattern, value[:bounds[i]]) {
				return value[bounds[i]:], nil
			}
		}
	} else {
		for _, b := range bounds {
			if matchGlob(pattern, value[:b]) {
				return value[b:], nil
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

	bounds := runeBoundaries(value)
	if longest {
		for _, b := range bounds {
			if matchGlob(pattern, value[b:]) {
				return value[:b], nil
			}
		}
	} else {
		for i := len(bounds) - 1; i >= 0; i-- {
			if matchGlob(pattern, value[bounds[i]:]) {
				return value[:bounds[i]], nil
			}
		}
	}

	return value, nil
}

func (p *parser) replace(value, pattern, replacement []byte, all bool, anchor byte) []byte {
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

	return out
}

func (p *parser) substring(characterStart int, value, spec []byte) ([]byte, error) {
	offset, rest, ok := parseIndex(spec)
	if !ok {
		return nil, p.newParserError(characterStart, "bad substitution: invalid substring")
	}

	length := utf8.RuneCount(value)
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
		return value[byteOffset(value, begin):], nil
	}
	if rest[0] != ':' {
		return nil, p.newParserError(characterStart, "bad substitution: invalid substring")
	}

	end, tail, ok := parseIndex(rest[1:])
	if !ok || len(bytes.TrimSpace(tail)) != 0 {
		return nil, p.newParserError(characterStart, "bad substitution: invalid substring length")
	}
	if end >= 0 {
		if end > length-begin {
			end = length
		} else {
			end += begin
		}
	} else {
		end += length
		if end < begin {
			end = begin
		}
	}

	return value[byteOffset(value, begin):byteOffset(value, end)], nil
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

func findMatch(pattern, str []byte, anchor byte) (start, end int, ok bool) {
	if !bytes.ContainsAny(pattern, "*?[\\") {
		switch anchor {
		case '#':
			if bytes.HasPrefix(str, pattern) {
				return 0, len(pattern), true
			}
		case '%':
			if bytes.HasSuffix(str, pattern) {
				return len(str) - len(pattern), len(str), true
			}
		default:
			if i := bytes.Index(str, pattern); i >= 0 {
				return i, i + len(pattern), true
			}
		}

		return 0, 0, false
	}

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
			_, size := utf8.DecodeRune(s[si:])
			pi++
			si += size
		case pi < len(pattern) && pattern[pi] == '[':
			next, matched, ok := matchClass(pattern, pi, s[si])
			switch {
			case ok && matched:
				pi = next
				si++
			case !ok && s[si] == '[':
				pi++
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

func matchClass(pattern []byte, start int, c byte) (next int, matched, ok bool) {
	i := start + 1
	negate := false
	if i < len(pattern) && (pattern[i] == '^' || pattern[i] == '!') {
		negate = true
		i++
	}

	matched = false
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
		return 0, false, false
	}

	return i + 1, matched != negate, true
}

// parseAnsiC decodes a bash `$'...'` ANSI-C quoted string, with the cursor at
// the opening quote.
func (p *parser) parseAnsiC(c *cursor) ([]byte, error) {
	quote := c.pos
	out := make([]byte, 0, 16)
	c.advance(1)

	for !c.eof() {
		switch ch := c.peek(); {
		case ch == '\'':
			c.advance(1)
			return out, nil
		case ch != '\\':
			out = append(out, ch)
			c.advance(1)
		case c.pos+1 >= len(c.data):
			return nil, p.newParserError(c.pos, "incomplete escape sequence")
		default:
			decoded, width, err := decodeEscape(c.data, c.pos+1)
			if err != nil {
				return nil, err
			}
			out = append(out, decoded...)
			c.advance(1 + width)
		}
	}

	return nil, p.newParserError(quote, "unmatched single quote")
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
