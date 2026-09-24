package out

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ValueText is a setting's value as a message shows it: compact JSON with no HTML escaping, or
// (unset) for none.
func ValueText(v any) string {
	if v == nil {
		return "(unset)"
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSuffix(b.String(), "\n")
}
