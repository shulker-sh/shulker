package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"shulker.sh/shulker/internal/out"
)

// Invalid turns a parse or schema failure for data read from file into one
// coded error: syntax errors carry file:line:column and plain words, schema
// errors one item per failing path.
func Invalid(code, file string, data []byte, err error) *out.Error {
	var syntax *json.SyntaxError
	var typed *json.UnmarshalTypeError
	var schemaErr *jsonschema.ValidationError
	var trailing *trailingError
	switch {
	case errors.Is(err, io.EOF) && len(bytes.TrimSpace(data)) == 0:
		return out.Errorf(code, "%s is empty", file)
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return out.Errorf(code, "%s: the file ends before the value is complete", position(file, data, int64(len(data))))
	case errors.As(err, &syntax):
		return out.Errorf(code, "%s: %s", position(file, data, syntax.Offset), plainSyntax(data, syntax))
	case errors.As(err, &trailing):
		return out.Errorf(code, "%s: extra content after the closing %s", position(file, data, trailing.offset), trailing.after)
	case errors.As(err, &typed):
		what := typed.Value
		if typed.Field != "" {
			what = typed.Field + " is " + what
		}
		return out.Errorf(code, "%s: %s, want %s", position(file, data, typed.Offset), what, typed.Type)
	case errors.As(err, &schemaErr):
		return schemaProblems(code, file, schemaErr)
	}
	return out.Errorf(code, "%s: %v", file, err)
}

// Decode parses data into a generic value the way the schema library does,
// keeping numbers exact, and reports content left after the value.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	end := dec.InputOffset()
	if rest := bytes.TrimLeft(data[end:], " \t\r\n"); len(rest) > 0 {
		offset := int64(len(data)-len(rest)) + 1
		return nil, &trailingError{offset: offset, after: closing(data[:end])}
	}
	return doc, nil
}

type trailingError struct {
	offset int64
	after  string
}

func (e *trailingError) Error() string { return "extra content after the top-level value" }

func closing(consumed []byte) string {
	switch trimmed := bytes.TrimRight(consumed, " \t\r\n"); {
	case bytes.HasSuffix(trimmed, []byte("}")):
		return "brace"
	case bytes.HasSuffix(trimmed, []byte("]")):
		return "bracket"
	}
	return "value"
}

func position(file string, data []byte, offset int64) string {
	at := int(offset) - 1
	if at < 0 {
		at = 0
	}
	if at > len(data) {
		at = len(data)
	}
	line := 1 + bytes.Count(data[:at], []byte("\n"))
	column := 1 + at - (bytes.LastIndexByte(data[:at], '\n') + 1)
	return fmt.Sprintf("%s:%d:%d", file, line, column)
}

var syntaxWords = []struct{ from, to string }{
	{"looking for beginning of object key string", "where a quoted key should start"},
	{"looking for beginning of value", "where a value should start"},
	{"after object key:value pair", "after a value; expected , or }"},
	{"after object key", "after a key; expected :"},
	{"after array element", "after a list item; expected , or ]"},
	{"after top-level value", "after the top-level value"},
	{"in string literal", "inside a string; escape it as \\n or close the string first"},
	{"in numeric literal", "inside a number"},
}

func plainSyntax(data []byte, e *json.SyntaxError) string {
	msg := e.Error()
	if msg == "unexpected end of JSON input" {
		return "the file ends before the value is complete"
	}
	at := int(e.Offset) - 1
	if at >= 0 && at < len(data) && (data[at] == '}' || data[at] == ']') {
		if before := bytes.TrimRight(data[:at], " \t\r\n"); bytes.HasSuffix(before, []byte(",")) {
			return fmt.Sprintf("trailing comma before %c", data[at])
		}
	}
	for _, w := range syntaxWords {
		if strings.HasSuffix(msg, w.from) {
			return strings.TrimSuffix(msg, w.from) + w.to
		}
	}
	return msg
}

func schemaProblems(code, file string, ve *jsonschema.ValidationError) *out.Error {
	printer := message.NewPrinter(language.English)
	var problems []string
	var collect func(*jsonschema.ValidationError)
	collect = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, cause := range e.Causes {
				collect(cause)
			}
			return
		}
		where := strings.Join(e.InstanceLocation, ".")
		if where == "" {
			where = file
		}
		problems = append(problems, where+": "+e.ErrorKind.LocalizedString(printer))
	}
	collect(ve)
	slices.Sort(problems)
	problems = slices.Compact(problems)
	if len(problems) == 1 {
		if strings.HasPrefix(problems[0], file+": ") {
			return out.Errorf(code, "%s", problems[0])
		}
		return out.Errorf(code, "%s: %s", file, problems[0])
	}
	e := out.Errorf(code, "%s has %d problems", file, len(problems))
	e.Items = problems
	return e
}
