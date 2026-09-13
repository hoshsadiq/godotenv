package godotenv

import (
	"bytes"
	"fmt"
	"strconv"
	"unicode/utf8"
)

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
