package godotenv

import (
	"bytes"
	"fmt"
)

type parserError struct {
	lineNumber int
	column     int

	message string
}

func (p parserError) Error() string {
	return fmt.Sprintf("godotenv: %s on line %d, column %d", p.message, p.lineNumber, p.column)
}

func (p *parser) newParserError(characterNumber int, message string) parserError {
	position := min(max(characterNumber, 0), len(p.data))

	lineStart := bytes.LastIndexByte(p.data[:position], '\n') + 1

	return parserError{
		lineNumber: bytes.Count(p.data[:lineStart], []byte("\n")) + 1,
		column:     position - lineStart + 1,
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
