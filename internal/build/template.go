package build

import (
	"bytes"
	"regexp"

	"github.com/andrewmast/shulker/internal/out"
)

var varRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func render(name string, data []byte, vars map[string]string) ([]byte, error) {
	var firstErr error
	rendered := varRe.ReplaceAllFunc(data, func(match []byte) []byte {
		key := string(varRe.FindSubmatch(match)[1])
		val, ok := vars[key]
		if !ok {
			if firstErr == nil {
				line := 1 + bytes.Count(data[:bytes.Index(data, match)], []byte("\n"))
				firstErr = out.Errorf("unset-variable", "%s:%d: variable ${%s} is not set", name, line, key)
			}
			return match
		}
		return []byte(val)
	})
	return rendered, firstErr
}
