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
			res, skip, err := p.resolveParameter(c.pos, c.data[c.pos+1:], true, lookupEnv)
			if err != nil {
				return err
			}

			p.value = append(p.value, res...)
			c.advance(1 + skip)
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
			res, skip, err := p.resolveParameter(c.pos, c.data[c.pos+1:], false, lookupEnv)
			if err != nil {
				return err
			}

			p.value = append(p.value, res...)
			c.advance(1 + skip)
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

// resolveParameter resolves the parameter starting at s and reports how many
// bytes it consumed. It handles $NAME and the ${NAME<op><word>} forms from the
// POSIX shell parameter expansion rules.
func (p *parser) resolveParameter(characterStart int, s []byte, allowANSI bool, lookupEnv LookupEnvFunc) (res []byte, skip int, err error) {
	if len(s) == 0 {
		return []byte("$"), 0, nil
	}

	switch {
	case s[0] == '{':
		return p.expandBracedRef(characterStart, s, lookupEnv)
	case p.cfg.restricted:
		return p.resolveName(characterStart, s, lookupEnv)
	case s[0] == '(':
		// Command substitution is never executed. Preserve `$(...)` verbatim,
		// taking nested parentheses and quoted strings into account.
		depth := 0
		var quote byte

		for i := 0; i < len(s); i++ {
			switch c := s[i]; {
			case c == '\n':
				return []byte("$"), 0, nil
			case quote == '\'':
				if c == '\'' {
					quote = 0
				}
			case quote == '"':
				switch c {
				case '\\':
					i++
				case '"':
					quote = 0
				}
			case c == '\'' || c == '"':
				quote = c
			case c == '\\':
				i++
			case c == '(':
				depth++
			case c == ')':
				depth--
				if depth == 0 {
					return append([]byte("$"), s[:i+1]...), i + 1, nil
				}
			}
		}

		return []byte("$"), 0, nil
	case allowANSI && s[0] == '\'':
		return p.ansiCString(characterStart, s)
	case allowANSI && s[0] == '"':
		return nil, 0, nil
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
// brace is never closed. Nested `${...}` and quoted strings are taken into
// account.
func matchBrace(s []byte) int {
	depth := 0
	var quote byte

	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
		case quote == '"':
			switch c {
			case '\\':
				i++
			case '"':
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '\\':
			i++
		case c == '{':
			depth++
		case c == '}':
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
		name := inner[1:]

		var i int
		if isAlpha(name[0]) || name[0] == '_' {
			for i = 1; i < len(name) && isAlphaNum(name[i]); i++ {
			}
		}
		if i == 0 || i != len(name) {
			return nil, p.newParserError(characterStart, "bad substitution")
		}

		v, envSet := lookupEnv(name)
		if !envSet && p.cfg.unboundErr {
			return nil, p.newUnboundVariable(characterStart, string(name))
		}

		return []byte(strconv.Itoa(utf8.RuneCount(v))), nil
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

		res, skip, err := p.resolveParameter(j, data[j+1:], true, lookupEnv)
		if err != nil {
			return "", err
		}
		out = append(out, res...)
		j += skip
	}

	return string(out), nil
}

func (p *parser) expandWord(characterStart int, w []byte, lookupEnv LookupEnvFunc) ([]byte, error) {
	if p.depth >= maxExpansionDepth {
		return nil, p.newParserError(characterStart, "expansion nesting too deep")
	}
	p.depth++
	defer func() { p.depth-- }()

	out := make([]byte, 0, len(w))
	for j := 0; j < len(w); {
		switch w[j] {
		case '\\':
			j++
			if j < len(w) {
				out = append(out, w[j])
				j++
			}
		case '\'':
			for j++; j < len(w) && w[j] != '\''; j++ {
				out = append(out, w[j])
			}
			if j < len(w) {
				j++
			}
		case '"':
			for j++; j < len(w) && w[j] != '"'; {
				if w[j] == '\\' && j+1 < len(w) {
					j++
					switch w[j] {
					case 'n':
						out = append(out, '\n')
					case 't':
						out = append(out, '\t')
					default:
						out = append(out, w[j])
					}
					j++
					continue
				}

				if w[j] == '$' {
					res, skip, err := p.resolveParameter(characterStart+j, w[j+1:], false, lookupEnv)
					if err != nil {
						return nil, err
					}
					out = append(out, res...)
					j += skip + 1
					continue
				}

				out = append(out, w[j])
				j++
			}
			if j < len(w) {
				j++
			}
		case '$':
			res, skip, err := p.resolveParameter(characterStart+j, w[j+1:], true, lookupEnv)
			if err != nil {
				return nil, err
			}
			out = append(out, res...)
			j += skip + 1
		default:
			out = append(out, w[j])
			j++
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
