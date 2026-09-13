package godotenv

import (
	"bytes"
	"fmt"
	"strings"
)

type parserError struct {
	lineNumber int
	column     int

	line    []byte
	message string
}

func (p parserError) Error() string {
	return fmt.Sprintf("godotenv: %s on line %d\n\t%s\n\t%s%s", p.message, p.lineNumber, p.line, strings.Repeat(" ", p.column-1), "^ Right here")
}

func (p *parser) newParserError(characterNumber int, message string) parserError {
	position := min(max(characterNumber, 0), len(p.data))

	lineStart := bytes.LastIndexByte(p.data[:position], '\n') + 1
	line := p.data[lineStart:]
	if end := bytes.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	line = bytes.TrimSuffix(line, []byte("\r"))

	return parserError{
		lineNumber: bytes.Count(p.data[:lineStart], []byte("\n")) + 1,
		column:     position - lineStart + 1,
		line:       line,
		message:    message,
	}
}

type unboundVariableError struct {
	parserError
	variableName string
}

func (p *parser) newUnboundVariable(characterNumber int, variableName string) unboundVariableError {
	return unboundVariableError{
		parserError:  p.newParserError(characterNumber, fmt.Sprintf("%s: unbound variable", variableName)),
		variableName: variableName,
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
