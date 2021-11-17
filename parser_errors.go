package godotenv

import (
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

func newParserError(line []byte, lineNumber int, characterNumber int, message string) parserError {
	return parserError{
		lineNumber:      lineNumber,
		characterNumber: characterNumber,
		line:            line,
		message:         message,
	}
}

type invalidCharacterError struct {
	parserError
	char byte
}

func newInvalidCharacterError(line []byte, lineNumber int, characterNumber int, char byte) invalidCharacterError {
	return invalidCharacterError{
		parserError: newParserError(line, lineNumber, characterNumber, fmt.Sprintf("invalid value character: 0x%0.2x", char)),
		char:        char,
	}
}
