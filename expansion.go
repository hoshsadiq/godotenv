package godotenv

import (
	"bytes"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/hoshsadiq/godotenv/glob"
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
		// allowANSI is false only in a double-quoted context, where the word
		// operands of ${...} treat a single quote as an ordinary byte.
		return p.parseBraced(c, !allowANSI, lookupEnv)
	case p.cfg.bash && allowANSI && (ch == '\'' || ch == '"'):
		return p.expandBashQuote(c)
	case p.cfg.restricted:
		return p.expandName(c, dollar, lookupEnv)
	case ch == '(' && c.peekAt(1) == '(':
		return p.parseArithmetic(c, dollar, lookupEnv)
	case ch == '(':
		end, ok := scanCommandSub(c.data, c.pos)
		if !ok {
			return []byte("$"), nil
		}

		res := append([]byte("$"), c.data[c.pos:end]...)
		c.pos = end

		return res, nil
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

// scanArithmetic returns the offset of the first ')' in the '))' that closes a
// `$((...))`, with start just past the opening '(('. A newline before the close
// is not spanned, and a ')' at depth zero not followed by another ')' is an
// unbalanced expression rather than a close.
func scanArithmetic(data []byte, start int) (close int, ok bool) {
	depth := 0
	for i := start; i < len(data); i++ {
		switch data[i] {
		case '\n':
			return 0, false
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
				continue
			}
			if i+1 < len(data) && data[i+1] == ')' {
				return i, true
			}
			return 0, false
		}
	}

	return 0, false
}

// parseArithmetic evaluates a `$((...))` expansion. The arithmetic itself is
// never executed by a shell; it is parsed and evaluated by evalArith.
func (p *parser) parseArithmetic(c *cursor, dollar int, lookupEnv LookupEnvFunc) ([]byte, error) {
	if p.depth >= maxExpansionDepth {
		return nil, p.newParserError(c.pos, "expansion nesting too deep")
	}
	p.depth++
	defer func() { p.depth-- }()

	open := c.pos + 1
	exprStart := open + 1
	closeIdx, ok := scanArithmetic(c.data, exprStart)
	if !ok {
		return nil, p.newParserError(dollar, "unexpected EOF while looking for matching '))'")
	}

	expr := c.data[exprStart:closeIdx]
	c.pos = closeIdx + 2

	if len(bytes.TrimSpace(expr)) == 0 {
		if p.cfg.bash {
			return []byte("0"), nil
		}
		return nil, p.newParserError(dollar, "bad substitution: invalid arithmetic expression")
	}

	expanded, err := p.expandArithPart(expr, lookupEnv)
	if err != nil {
		return nil, err
	}
	value, ok := evalArith(expanded, lookupEnv)
	if !ok {
		return nil, p.newParserError(dollar, "bad substitution: invalid arithmetic expression")
	}

	return strconv.AppendInt(nil, int64(value), 10), nil
}

func (p *parser) parseBraced(c *cursor, quoted bool, lookupEnv LookupEnvFunc) ([]byte, error) {
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
		if !hasClosingBraceAt(c, c.pos) {
			return nil, p.newParserError(brace, "unexpected EOF while looking for matching '}'")
		}
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
		if c.eof() {
			return nil, p.newParserError(brace, "unexpected EOF while looking for matching '}'")
		}
		if c.peek() == '}' {
			return nil, p.newParserError(nameEnd, "bad substitution: no modifier")
		}
	}

	opPos := c.pos

	if colon {
		switch op := c.peek(); op {
		case '-':
			c.advance(1)
			if !envSet || len(value) == 0 {
				return p.parseWordUntilBrace(c, brace, quoted, lookupEnv)
			}
			if err := p.skipWord(c, brace, quoted); err != nil {
				return nil, err
			}
			return value, nil
		case '+':
			c.advance(1)
			if len(value) > 0 {
				return p.parseWordUntilBrace(c, brace, quoted, lookupEnv)
			}
			if err := p.skipWord(c, brace, quoted); err != nil {
				return nil, err
			}
			return nil, nil
		case '?':
			c.advance(1)
			if !envSet || len(value) == 0 {
				return p.parameterError(c, brace, name, quoted, lookupEnv)
			}
			if err := p.skipWord(c, brace, quoted); err != nil {
				return nil, err
			}
			return value, nil
		case '=':
			if !hasClosingBraceAt(c, c.pos) {
				return nil, p.newParserError(brace, "unexpected EOF while looking for matching '}'")
			}
			return nil, p.newParserError(opPos, "bad substitution: assignment is not supported")
		default:
			if !envSet && p.cfg.unboundErr {
				return nil, p.newUnboundVariable(brace, string(name))
			}
			return p.expandSubstring(c, brace, opPos, value, lookupEnv)
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
			return p.parseWordUntilBrace(c, brace, quoted, lookupEnv)
		}
		if err := p.skipWord(c, brace, quoted); err != nil {
			return nil, err
		}
		return value, nil
	case '+':
		c.advance(1)
		if envSet {
			return p.parseWordUntilBrace(c, brace, quoted, lookupEnv)
		}
		if err := p.skipWord(c, brace, quoted); err != nil {
			return nil, err
		}
		return nil, nil
	case '?':
		c.advance(1)
		if !envSet {
			return p.parameterError(c, brace, name, quoted, lookupEnv)
		}
		if err := p.skipWord(c, brace, quoted); err != nil {
			return nil, err
		}
		return value, nil
	case '=':
		if !hasClosingBraceAt(c, c.pos) {
			return nil, p.newParserError(brace, "unexpected EOF while looking for matching '}'")
		}
		return nil, p.newParserError(opPos, "bad substitution: assignment is not supported")
	case '#', '%':
		if !envSet && p.cfg.unboundErr {
			return nil, p.newUnboundVariable(brace, string(name))
		}
		return p.expandStrip(c, brace, op, value, lookupEnv)
	case '/':
		if !envSet && p.cfg.unboundErr {
			return nil, p.newUnboundVariable(brace, string(name))
		}
		return p.expandReplace(c, brace, value, lookupEnv)
	default:
		if !hasClosingBraceAt(c, c.pos) {
			return nil, p.newParserError(brace, "unexpected EOF while looking for matching '}'")
		}
		return nil, p.newParserError(opPos, "bad substitution: unsupported operator")
	}
}

func (p *parser) parseLength(c *cursor, brace int, lookupEnv LookupEnvFunc) ([]byte, error) {
	c.advance(1)

	name := p.parseName(c)
	if !hasClosingBraceAt(c, c.pos) {
		return nil, p.newParserError(brace, "unexpected EOF while looking for matching '}'")
	}
	if len(name) == 0 || c.peek() != '}' {
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

func (p *parser) parseWordUntilBrace(c *cursor, brace int, quoted bool, lookupEnv LookupEnvFunc) ([]byte, error) {
	start := c.pos

	word, err := p.parseWord(c, quoted, lookupEnv)
	if err != nil {
		// The innermost frame decides whether the expansion is unclosed; the
		// outer frames would only repeat the same scan.
		if !p.braceChecked {
			p.braceChecked = true
			if !hasClosingBraceAt(c, start) {
				return nil, p.newParserError(brace, "unexpected EOF while looking for matching '}'")
			}
		}
		return nil, err
	}
	if err := p.consumeBrace(c, brace); err != nil {
		return nil, err
	}

	return word, nil
}

func (p *parser) skipWord(c *cursor, brace int, quoted bool) error {
	scanRawMode(c, !quoted || p.cfg.bash)
	return p.consumeBrace(c, brace)
}

func (p *parser) consumeBrace(c *cursor, brace int) error {
	if c.eof() || c.peek() != '}' {
		return p.newParserError(brace, "unexpected EOF while looking for matching '}'")
	}
	c.advance(1)

	return nil
}

func (p *parser) parameterError(c *cursor, brace int, name []byte, quoted bool, lookupEnv LookupEnvFunc) ([]byte, error) {
	word, err := p.parseWord(c, quoted, lookupEnv)
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

func (p *parser) expandSubstring(c *cursor, brace, opPos int, value []byte, lookupEnv LookupEnvFunc) ([]byte, error) {
	spec := scanRaw(c)
	if err := p.consumeBrace(c, brace); err != nil {
		return nil, err
	}

	return p.substring(opPos, value, spec, lookupEnv)
}

func (p *parser) expandStrip(c *cursor, brace int, op byte, value []byte, lookupEnv LookupEnvFunc) ([]byte, error) {
	longest := false
	c.advance(1)
	if !c.eof() && c.peek() == op {
		longest = true
		c.advance(1)
	}

	pattern, err := p.parsePatternWord(c, brace, false, lookupEnv)
	if err != nil {
		return nil, err
	}
	if err := p.consumeBrace(c, brace); err != nil {
		return nil, err
	}

	if op == '#' {
		return stripPrefix(value, pattern, longest), nil
	}

	return stripSuffix(value, pattern, longest), nil
}

func (p *parser) expandReplace(c *cursor, brace int, value []byte, lookupEnv LookupEnvFunc) ([]byte, error) {
	c.advance(1)

	all := false
	if c.peek() == '/' {
		all = true
		c.advance(1)
	}

	// The #/% anchors only apply to the single-slash form; in ${V//pat/rep}
	// they are ordinary pattern characters.
	var anchor byte
	if !all && (c.peek() == '#' || c.peek() == '%') {
		anchor = c.peek()
		c.advance(1)
	}

	// In ${V///pat/rep} the pattern itself begins with '/', and bash skips that
	// leading '/' when looking for the replacement delimiter, so it is part of
	// the pattern rather than an empty-pattern separator.
	var pattern []byte
	if all && c.peek() == '/' {
		pattern = append(pattern, '/')
		c.advance(1)
	}

	rest, err := p.parsePatternWord(c, brace, true, lookupEnv)
	if err != nil {
		return nil, err
	}
	pattern = append(pattern, rest...)

	var replacement []byte
	if c.peek() == '/' {
		c.advance(1)

		if replacement, err = p.parseWord(c, false, lookupEnv); err != nil {
			return nil, err
		}
	}

	if err := p.consumeBrace(c, brace); err != nil {
		return nil, err
	}

	return p.replace(value, pattern, replacement, all, anchor), nil
}

// parsePatternWord parses the pattern operand of a strip or replace expansion.
// Parameter expansions are resolved and treated as pattern text, while quoted
// metacharacters are escaped so they match literally. It stops at the closing
// brace, or at an unquoted '/' when stopAtSlash is set.
func (p *parser) parsePatternWord(c *cursor, brace int, stopAtSlash bool, lookupEnv LookupEnvFunc) ([]byte, error) {
	if p.depth >= maxExpansionDepth {
		return nil, p.newParserError(c.pos, "expansion nesting too deep")
	}
	p.depth++
	defer func() { p.depth-- }()

	out := make([]byte, 0, 16)

	for !c.eof() {
		switch ch := c.peek(); ch {
		case '}':
			return out, nil
		case '/':
			if stopAtSlash {
				return out, nil
			}
			out = appendPatternByte(out, ch, false)
			c.advance(1)
		case '\\':
			c.advance(1)
			if c.eof() {
				out = appendPatternByte(out, '\\', false)
				break
			}
			out = appendPatternByte(out, c.peek(), true)
			c.advance(1)
		case '\'':
			c.advance(1)
			for !c.eof() && c.peek() != '\'' {
				out = appendPatternByte(out, c.peek(), true)
				c.advance(1)
			}
			if c.eof() {
				return nil, p.newParserError(brace, "unmatched single quote")
			}
			c.advance(1)
		case '"':
			if err := p.parsePatternDoubleQuoted(c, &out, brace, lookupEnv); err != nil {
				return nil, err
			}
		case '$':
			if err := p.parsePatternDollar(c, &out, brace, lookupEnv); err != nil {
				return nil, err
			}
		default:
			out = appendPatternByte(out, ch, false)
			c.advance(1)
		}
	}

	return out, nil
}

func (p *parser) parsePatternDollar(c *cursor, out *[]byte, brace int, lookupEnv LookupEnvFunc) error {
	if p.cfg.bash {
		if handled, err := p.parsePatternBashDollar(c, out, brace, lookupEnv); handled {
			return err
		}
	}

	res, err := p.expandDollar(c, true, lookupEnv)
	if err != nil {
		return err
	}
	*out = append(*out, res...)

	return nil
}

func (p *parser) parsePatternDoubleQuoted(c *cursor, out *[]byte, brace int, lookupEnv LookupEnvFunc) error {
	c.advance(1)

	return p.parsePatternQuoted(c, out, '"', brace, lookupEnv)
}

// parsePatternQuoted copies a double-quote-context pattern body up to term,
// expanding parameter references and escaping the pattern bytes they produce.
func (p *parser) parsePatternQuoted(c *cursor, out *[]byte, term byte, brace int, lookupEnv LookupEnvFunc) error {
	for !c.eof() {
		switch ch := c.peek(); ch {
		case term:
			c.advance(1)
			return nil
		case '\\':
			c.advance(1)
			if c.eof() {
				return p.newParserError(brace, "unmatched quote")
			}
			switch next := c.peek(); next {
			case '$', '`', '"', '\\':
				*out = appendPatternByte(*out, next, true)
				c.advance(1)
			case '\r':
				if c.peekAt(1) == '\n' {
					c.advance(2)
				} else {
					*out = appendPatternByte(*out, '\\', true)
					c.advance(1)
				}
			case '\n':
				c.advance(1)
			default:
				*out = appendPatternByte(*out, '\\', true)
				c.advance(1)
			}
		case '$':
			res, err := p.expandDollar(c, false, lookupEnv)
			if err != nil {
				return err
			}
			for _, b := range res {
				*out = appendPatternByte(*out, b, true)
			}
		default:
			*out = appendPatternByte(*out, ch, true)
			c.advance(1)
		}
	}

	return p.newParserError(brace, "unmatched quote")
}

func appendPatternByte(out []byte, b byte, quoted bool) []byte {
	if quoted {
		switch b {
		case '*', '?', '[', '\\':
			out = append(out, '\\')
		}
	}

	return append(out, b)
}

func scanRaw(c *cursor) []byte {
	return scanRawMode(c, true)
}

// scanRawMode advances the cursor past a ${...} word without expanding it,
// returning the raw text. A single quote opens a quoted region only when
// singleQuote is set: the word of a double-quoted ${...} treats it as an
// ordinary byte in POSIX mode, so the region it would open must not hide a '}'.
func scanRawMode(c *cursor, singleQuote bool) []byte {
	start := c.pos
	depth := 0
	var quote byte

	for !c.eof() {
		switch ch := c.peek(); {
		case quote == '\'':
			if ch == '\'' {
				quote = 0
			}
		case quote == '"':
			if ch == '\\' {
				c.advance(2)
				continue
			}
			if ch == '"' {
				quote = 0
			}
		case ch == '"':
			quote = '"'
		case ch == '\'' && singleQuote:
			quote = '\''
		case ch == '\\':
			c.advance(2)
			continue
		case ch == '$' && c.peekAt(1) == '{':
			depth++
			c.advance(2)
			continue
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

func hasClosingBraceAt(c *cursor, start int) bool {
	pos := c.pos
	c.pos = start
	scanRaw(c)
	ok := !c.eof() && c.peek() == '}'
	c.pos = pos

	return ok
}

func (p *parser) parseWord(c *cursor, quoted bool, lookupEnv LookupEnvFunc) ([]byte, error) {
	if p.depth >= maxExpansionDepth {
		return nil, p.newParserError(c.pos, "expansion nesting too deep")
	}
	p.depth++
	defer func() { p.depth-- }()

	prefix := 0
	for c.pos+prefix < len(c.data) && !isWordSpecialByte(c.data[c.pos+prefix]) {
		prefix++
	}
	if c.pos+prefix < len(c.data) && c.data[c.pos+prefix] == '}' {
		c.advance(prefix)
		return c.data[c.pos-prefix : c.pos], nil
	}
	out := make([]byte, 0, prefix+16)

	for !c.eof() {
		switch ch := c.peek(); ch {
		case '}':
			return out, nil
		case '\\':
			if quoted {
				decoded, err := p.parseDoubleQuoteWordEscape(c)
				if err != nil {
					return nil, err
				}
				out = append(out, decoded...)
				continue
			}
			c.advance(1)
			if !c.eof() {
				out = append(out, c.peek())
				c.advance(1)
			}
		case '\'':
			if quoted && !p.cfg.bash {
				// POSIX: inside a double-quoted ${...} a single quote is an
				// ordinary byte, so it does not hide a '}' from the brace
				// matcher and the expansion ends at the first one.
				out = append(out, '\'')
				c.advance(1)
				continue
			}
			if quoted {
				// bash groups the quote region and keeps the quotes, but the
				// region still hides a '}' from the brace matcher.
				out = append(out, '\'')
				c.advance(1)

				var err error
				out, err = p.parseWordQuotedBody(c, out, '\'', lookupEnv)
				if err != nil {
					return nil, err
				}
				if !c.eof() {
					out = append(out, '\'')
					c.advance(1)
				}
				continue
			}

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

			var err error
			out, err = p.parseWordQuotedBody(c, out, '"', lookupEnv)
			if err != nil {
				return nil, err
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

// parseWordQuotedBody copies a double-quote-context body up to term, expanding
// parameter references and applying the same backslash handling as "...".
func (p *parser) parseWordQuotedBody(c *cursor, out []byte, term byte, lookupEnv LookupEnvFunc) ([]byte, error) {
	for !c.eof() && c.peek() != term {
		switch c.peek() {
		case '\\':
			decoded, err := p.parseDoubleQuoteEscape(c)
			if err != nil {
				return nil, err
			}
			out = append(out, decoded...)
		case '$':
			res, err := p.expandDollar(c, false, lookupEnv)
			if err != nil {
				return nil, err
			}
			out = append(out, res...)
		default:
			out = append(out, c.peek())
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

func stripPrefix(value, pattern []byte, longest bool) []byte {
	bounds := runeBoundaries(value)
	if longest {
		for i := len(bounds) - 1; i >= 0; i-- {
			if glob.Match(pattern, value[:bounds[i]]) {
				return value[bounds[i]:]
			}
		}
	} else {
		for _, b := range bounds {
			if glob.Match(pattern, value[:b]) {
				return value[b:]
			}
		}
	}

	return value
}

func stripSuffix(value, pattern []byte, longest bool) []byte {
	bounds := runeBoundaries(value)
	if longest {
		for _, b := range bounds {
			if glob.Match(pattern, value[b:]) {
				return value[:b]
			}
		}
	} else {
		for i := len(bounds) - 1; i >= 0; i-- {
			if glob.Match(pattern, value[bounds[i]:]) {
				return value[:bounds[i]]
			}
		}
	}

	return value
}

func (p *parser) replace(value, pattern, replacement []byte, all bool, anchor byte) []byte {
	if len(pattern) == 0 {
		switch anchor {
		case '#':
			out := make([]byte, 0, len(replacement)+len(value))
			out = append(out, replacement...)
			return append(out, value...)
		case '%':
			out := make([]byte, 0, len(value)+len(replacement))
			out = append(out, value...)
			return append(out, replacement...)
		default:
			return value
		}
	}

	if all && anchor == 0 {
		return p.replaceAll(value, pattern, replacement)
	}

	var start, end int
	var ok bool
	switch anchor {
	case '#':
		start, end, ok = glob.MatchPrefix(pattern, value)
	case '%':
		start, end, ok = glob.MatchSuffix(pattern, value)
	default:
		start, end, ok = glob.Find(pattern, value)
	}
	if !ok {
		return value
	}

	out := make([]byte, 0, len(value)-end+start+len(replacement))
	out = append(out, value[:start]...)
	out = append(out, replacement...)

	return append(out, value[end:]...)
}

// replaceAll replaces every longest non-overlapping match, walking the value
// left to right. A match that reaches the end of the value is not followed by
// a further empty match, but an empty value still gets one attempt so a
// pattern matching the empty string (such as `*`) inserts a replacement.
func (p *parser) replaceAll(value, pattern, replacement []byte) []byte {
	if len(value) == 0 {
		if _, end, ok := glob.MatchPrefix(pattern, value); ok && end == 0 {
			return append([]byte(nil), replacement...)
		}
		return value
	}

	out := make([]byte, 0, len(value))
	offset := 0

	for offset < len(value) {
		_, end, ok := glob.MatchPrefix(pattern, value[offset:])
		if !ok || end == 0 {
			_, size := utf8.DecodeRune(value[offset:])
			out = append(out, value[offset:offset+size]...)
			offset += size
			continue
		}

		out = append(out, replacement...)
		offset += end
	}

	return out
}

func (p *parser) substring(characterStart int, value, spec []byte, lookupEnv LookupEnvFunc) ([]byte, error) {
	offsetRaw, lengthRaw, hasLength := splitSubstring(spec)

	offsetExpr, err := p.expandArithPart(offsetRaw, lookupEnv)
	if err != nil {
		return nil, err
	}
	begin, ok := evalArith(offsetExpr, lookupEnv)
	if !ok {
		return nil, p.newParserError(characterStart, "bad substitution: invalid substring")
	}

	length := utf8.RuneCount(value)
	if begin < 0 {
		begin += length
	}
	if begin < 0 || begin > length {
		return nil, nil
	}
	if !hasLength {
		return value[byteOffset(value, begin):], nil
	}

	lengthExpr, err := p.expandArithPart(lengthRaw, lookupEnv)
	if err != nil {
		return nil, err
	}
	end, ok := evalArith(lengthExpr, lookupEnv)
	if !ok {
		return nil, p.newParserError(characterStart, "bad substitution: invalid substring length")
	}
	switch {
	case end < 0:
		end += length
		if end < 0 || end < begin {
			return nil, p.newParserError(characterStart, "bad substitution: negative substring length")
		}
	case end > length-begin:
		end = length
	default:
		end += begin
	}

	return value[byteOffset(value, begin):byteOffset(value, end)], nil
}

// expandArithPart resolves parameter references in one operand of a substring
// expansion, leaving arithmetic operators and bare variable names for evalArith.
func (p *parser) expandArithPart(part []byte, lookupEnv LookupEnvFunc) ([]byte, error) {
	out := make([]byte, 0, len(part))
	c := &cursor{data: part}

	for !c.eof() {
		if c.peek() != '$' {
			out = append(out, c.peek())
			c.advance(1)
			continue
		}

		res, err := p.expandDollar(c, true, lookupEnv)
		if err != nil {
			return nil, err
		}
		out = append(out, res...)
	}

	return out, nil
}

// splitSubstring splits a substring operand at the top-level ':' that separates
// offset from length, following bash's skiparith rules: parenthesised groups and
// ${...} references are skipped whole, each '?' absorbs one ':', and a backslash
// or quote hides the delimiter.
func splitSubstring(spec []byte) (offset, length []byte, hasLength bool) {
	pending := 0

	for i := 0; i < len(spec); {
		switch ch := spec[i]; ch {
		case '\\':
			i += 2
		case '\'':
			i = skipQuoted(spec, i+1, '\'')
		case '"':
			i = skipQuoted(spec, i+1, '"')
		case '(':
			i = skipBalanced(spec, i, '(', ')')
		case '$':
			if i+1 < len(spec) && spec[i+1] == '{' {
				i = skipBalanced(spec, i+1, '{', '}')
			} else {
				i++
			}
		case '?':
			pending++
			i++
		case ':':
			if pending == 0 {
				return spec[:i], spec[i+1:], true
			}
			pending--
			i++
		default:
			i++
		}
	}

	return spec, nil, false
}

func skipQuoted(s []byte, i int, quote byte) int {
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case quote:
			return i + 1
		}
	}

	return i
}

func skipBalanced(s []byte, i int, open, close byte) int {
	depth := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}

	return i
}

// maxArithDepth bounds arithmetic recursion (parentheses, unary operators and
// recursive variable values) so hostile input cannot exhaust the stack.
const maxArithDepth = 10000

type arith struct {
	data   []byte
	pos    int
	lookup LookupEnvFunc
	depth  int
}

// evalArith evaluates a bash arithmetic expression. Bare names resolve through
// lookup, recursively and with unset variables as zero; ok is false on a syntax
// error such as division by zero or trailing input.
func evalArith(expr []byte, lookup LookupEnvFunc) (int, bool) {
	return evalArithDepth(expr, lookup, 0)
}

// evalArithDepth is evalArith carrying the current recursion depth, so nested
// evaluation of a variable's value cannot restart the depth budget.
func evalArithDepth(expr []byte, lookup LookupEnvFunc, depth int) (int, bool) {
	a := arith{data: expr, lookup: lookup, depth: depth}

	v, ok := a.ternary()
	if !ok {
		return 0, false
	}
	a.space()
	if a.pos != len(a.data) {
		return 0, false
	}

	return v, true
}

func (a *arith) space() {
	for a.pos < len(a.data) {
		switch a.data[a.pos] {
		case ' ', '\t', '\n', '\r':
			a.pos++
		default:
			return
		}
	}
}

func (a *arith) match(op string) bool {
	a.space()
	if a.pos+len(op) > len(a.data) || string(a.data[a.pos:a.pos+len(op)]) != op {
		return false
	}
	// A single '&' or '|' must not swallow the first byte of '&&' or '||',
	// which bind more loosely and are handled by an outer level.
	if len(op) == 1 && (op[0] == '&' || op[0] == '|') && a.pos+1 < len(a.data) && a.data[a.pos+1] == op[0] {
		return false
	}
	a.pos += len(op)

	return true
}

func (a *arith) ternary() (int, bool) {
	cond, ok := a.logicalOr()
	if !ok {
		return 0, false
	}
	if !a.match("?") {
		return cond, true
	}
	then, ok := a.ternary()
	if !ok {
		return 0, false
	}
	if !a.match(":") {
		return 0, false
	}
	els, ok := a.ternary()
	if !ok {
		return 0, false
	}
	if cond != 0 {
		return then, true
	}

	return els, true
}

func (a *arith) logicalOr() (int, bool) {
	v, ok := a.logicalAnd()
	if !ok {
		return 0, false
	}
	for a.match("||") {
		r, ok := a.logicalAnd()
		if !ok {
			return 0, false
		}
		v = boolToInt(v != 0 || r != 0)
	}

	return v, true
}

func (a *arith) logicalAnd() (int, bool) {
	v, ok := a.bitOr()
	if !ok {
		return 0, false
	}
	for a.match("&&") {
		r, ok := a.bitOr()
		if !ok {
			return 0, false
		}
		v = boolToInt(v != 0 && r != 0)
	}

	return v, true
}

func (a *arith) bitOr() (int, bool) {
	v, ok := a.bitXor()
	if !ok {
		return 0, false
	}
	for a.match("|") {
		r, ok := a.bitXor()
		if !ok {
			return 0, false
		}
		v |= r
	}

	return v, true
}

func (a *arith) bitXor() (int, bool) {
	v, ok := a.bitAnd()
	if !ok {
		return 0, false
	}
	for a.match("^") {
		r, ok := a.bitAnd()
		if !ok {
			return 0, false
		}
		v ^= r
	}

	return v, true
}

func (a *arith) bitAnd() (int, bool) {
	v, ok := a.equality()
	if !ok {
		return 0, false
	}
	for a.match("&") {
		r, ok := a.equality()
		if !ok {
			return 0, false
		}
		v &= r
	}

	return v, true
}

func (a *arith) equality() (int, bool) {
	v, ok := a.relational()
	if !ok {
		return 0, false
	}
	for {
		switch {
		case a.match("=="):
			r, ok := a.relational()
			if !ok {
				return 0, false
			}
			v = boolToInt(v == r)
		case a.match("!="):
			r, ok := a.relational()
			if !ok {
				return 0, false
			}
			v = boolToInt(v != r)
		default:
			return v, true
		}
	}
}

func (a *arith) relational() (int, bool) {
	v, ok := a.shift()
	if !ok {
		return 0, false
	}
	for {
		switch {
		case a.match("<="):
			r, ok := a.shift()
			if !ok {
				return 0, false
			}
			v = boolToInt(v <= r)
		case a.match(">="):
			r, ok := a.shift()
			if !ok {
				return 0, false
			}
			v = boolToInt(v >= r)
		case a.match("<"):
			r, ok := a.shift()
			if !ok {
				return 0, false
			}
			v = boolToInt(v < r)
		case a.match(">"):
			r, ok := a.shift()
			if !ok {
				return 0, false
			}
			v = boolToInt(v > r)
		default:
			return v, true
		}
	}
}

func (a *arith) shift() (int, bool) {
	v, ok := a.additive()
	if !ok {
		return 0, false
	}
	for {
		switch {
		case a.match("<<"):
			r, ok := a.additive()
			if !ok || r < 0 || r > 63 {
				return 0, false
			}
			v <<= r
		case a.match(">>"):
			r, ok := a.additive()
			if !ok || r < 0 || r > 63 {
				return 0, false
			}
			v >>= r
		default:
			return v, true
		}
	}
}

func (a *arith) additive() (int, bool) {
	v, ok := a.multiplicative()
	if !ok {
		return 0, false
	}
	for {
		switch {
		case a.match("+"):
			r, ok := a.multiplicative()
			if !ok {
				return 0, false
			}
			v += r
		case a.match("-"):
			r, ok := a.multiplicative()
			if !ok {
				return 0, false
			}
			v -= r
		default:
			return v, true
		}
	}
}

func (a *arith) multiplicative() (int, bool) {
	v, ok := a.unary()
	if !ok {
		return 0, false
	}
	for {
		switch {
		case a.match("*"):
			r, ok := a.unary()
			if !ok {
				return 0, false
			}
			v *= r
		case a.match("/"):
			r, ok := a.unary()
			if !ok || r == 0 {
				return 0, false
			}
			v /= r
		case a.match("%"):
			r, ok := a.unary()
			if !ok || r == 0 {
				return 0, false
			}
			v %= r
		default:
			return v, true
		}
	}
}

func (a *arith) unary() (int, bool) {
	var op byte
	switch {
	case a.match("+"):
		op = '+'
	case a.match("-"):
		op = '-'
	case a.match("!"):
		op = '!'
	case a.match("~"):
		op = '~'
	default:
		return a.primary()
	}
	if a.depth >= maxArithDepth {
		return 0, false
	}
	a.depth++
	defer func() { a.depth-- }()

	v, ok := a.unary()
	if !ok {
		return 0, false
	}
	switch op {
	case '-':
		return -v, true
	case '!':
		return boolToInt(v == 0), true
	case '~':
		return ^v, true
	}

	return v, true
}

func (a *arith) primary() (int, bool) {
	a.space()
	if a.pos >= len(a.data) {
		return 0, false
	}
	if a.data[a.pos] == '(' {
		if a.depth >= maxArithDepth {
			return 0, false
		}
		a.depth++
		defer func() { a.depth-- }()
		a.pos++
		v, ok := a.ternary()
		if !ok {
			return 0, false
		}
		if !a.match(")") {
			return 0, false
		}
		return v, true
	}
	if n, ok := a.number(); ok {
		return n, true
	}

	return a.name()
}

func (a *arith) number() (int, bool) {
	a.space()
	start := a.pos

	if a.pos+1 < len(a.data) && a.data[a.pos] == '0' && (a.data[a.pos+1] == 'x' || a.data[a.pos+1] == 'X') {
		a.pos += 2
		n, digits := 0, 0
		for a.pos < len(a.data) && isHex(a.data[a.pos]) {
			n = n*16 + hexVal(a.data[a.pos])
			a.pos++
			digits++
		}
		if digits == 0 {
			a.pos = start
			return 0, false
		}
		return n, true
	}

	n := 0
	for a.pos < len(a.data) && a.data[a.pos] >= '0' && a.data[a.pos] <= '9' {
		n = n*10 + int(a.data[a.pos]-'0')
		a.pos++
	}
	if a.pos == start {
		return 0, false
	}

	return n, true
}

func (a *arith) name() (int, bool) {
	a.space()
	if a.pos >= len(a.data) || (!isAlpha(a.data[a.pos]) && a.data[a.pos] != '_') {
		return 0, false
	}

	start := a.pos
	for a.pos < len(a.data) && isAlphaNum(a.data[a.pos]) {
		a.pos++
	}
	name := a.data[start:a.pos]

	value, ok := a.lookup(name)
	if !ok || len(value) == 0 {
		return 0, true
	}
	if a.depth >= maxArithDepth {
		return 0, false
	}

	return evalArithDepth(value, a.lookup, a.depth+1)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}
