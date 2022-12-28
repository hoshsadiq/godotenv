package godotenv

import (
	"bytes"
	"fmt"
	"strings"
)

type parserError struct {
	lineNumber      int
	characterNumber int

	line    []byte
	message string
}

func (p parserError) Error() string {
	return fmt.Sprintf("godotenv: %s on line %d\n\t%s\n\t%s%s", p.message, p.lineNumber, p.line, strings.Repeat(" ", p.characterNumber-1), "^ Right here")
}

func (p *parser) newParserError(characterNumber int, message string) parserError {
	// this is not ideal, but it's only on error, so should be okay, maybe?
	line := bytes.SplitN(p.data, []byte("\n"), p.lineNumber+1)[p.lineNumber-1]
	return parserError{
		lineNumber:      p.lineNumber,
		characterNumber: characterNumber,
		line:            line,
		message:         message,
	}
}

type invalidCharacterError struct {
	parserError
	char byte
}

func (p *parser) newInvalidCharacterError(characterNumber int, char byte) invalidCharacterError {
	return invalidCharacterError{
		parserError: p.newParserError(characterNumber, fmt.Sprintf("invalid value character: 0x%0.2x", char)),
		char:        char,
	}
}
