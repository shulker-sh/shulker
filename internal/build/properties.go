package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const PropertiesFile = "server.properties"

type properties map[string]string

func formatProperty(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case json.Number:
		return x.String(), nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	}
	return "", fmt.Errorf("unsupported property value %v (%T)", v, v)
}

func parseProperties(data []byte) properties {
	p := properties{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := splitProperty(line)
		if ok {
			p[key] = value
		}
	}
	return p
}

func splitProperty(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
		return "", "", false
	}
	i := strings.IndexAny(trimmed, "=:")
	if i < 0 {
		return strings.TrimSpace(trimmed), "", true
	}
	return strings.TrimSpace(trimmed[:i]), strings.TrimSpace(trimmed[i+1:]), true
}

func (p properties) keys() []string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (p properties) canonical() []byte {
	var buf bytes.Buffer
	for _, k := range p.keys() {
		fmt.Fprintf(&buf, "%s=%s\n", k, p[k])
	}
	return buf.Bytes()
}

func (p properties) restrict(existing properties) properties {
	sub := properties{}
	for k := range p {
		if v, ok := existing[k]; ok {
			sub[k] = v
		}
	}
	return sub
}

func (p properties) mergeInto(existing []byte) []byte {
	if len(existing) == 0 {
		return p.canonical()
	}
	pending := map[string]bool{}
	for k := range p {
		pending[k] = true
	}
	lines := strings.Split(strings.TrimRight(string(existing), "\n"), "\n")
	for i, line := range lines {
		key, _, ok := splitProperty(line)
		if !ok || !pending[key] {
			continue
		}
		lines[i] = key + "=" + p[key]
		delete(pending, key)
	}
	for _, k := range p.keys() {
		if pending[k] {
			lines = append(lines, k+"="+p[k])
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}
