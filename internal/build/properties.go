package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	PropertiesFile = "server.properties"
	OptionsFile    = "options.txt"
)

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

type propsFile struct {
	props   properties
	sep     string
	base    []byte
	origins map[string]keySource
}

type keySource struct {
	path     string
	pack     string
	template bool
}

func (f propsFile) keys() []string            { return f.props.keys() }
func (f propsFile) values() map[string]string { return map[string]string(f.props) }

func (f propsFile) existingValues(existing []byte) map[string]string {
	return map[string]string(parseProperties(existing))
}

func (f propsFile) render(existing []byte, kept, dropped map[string]bool) ([]byte, error) {
	if len(existing) == 0 {
		existing = f.base
	}
	write := properties{}
	for k, v := range f.props {
		if !kept[k] {
			write[k] = v
		}
	}
	return write.mergeInto(existing, f.sep, dropped), nil
}

func (p properties) mergeInto(existing []byte, sep string, dropped map[string]bool) []byte {
	if len(existing) == 0 {
		var buf bytes.Buffer
		for _, k := range p.keys() {
			buf.WriteString(k + sep + p[k] + "\n")
		}
		return buf.Bytes()
	}
	pending := map[string]bool{}
	for k := range p {
		pending[k] = true
	}
	lines := []string{}
	for _, line := range strings.Split(strings.TrimRight(string(existing), "\n"), "\n") {
		key, _, ok := splitProperty(line)
		switch {
		case ok && dropped[key]:
			continue
		case ok && pending[key]:
			line = key + sep + p[key]
			delete(pending, key)
		}
		lines = append(lines, line)
	}
	for _, k := range p.keys() {
		if pending[k] {
			lines = append(lines, k+sep+p[k])
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// Keys already set by the manifest (no origin path) stay on top; later layers
// replace keys from earlier ones.
func mergedProperties(prev source, data []byte, from keySource, src source) source {
	merged := properties{}
	origins := map[string]keySource{}
	sep := "="
	if pf, ok := prev.owned.(propsFile); ok {
		for k, v := range pf.props {
			merged[k] = v
		}
		for k, o := range pf.origins {
			origins[k] = o
		}
		sep = pf.sep
	}
	for k, v := range parseProperties(data) {
		if _, set := merged[k]; set && origins[k].path == "" {
			continue
		}
		merged[k] = v
		origins[k] = from
	}
	src.owned = propsFile{props: merged, sep: sep, base: data, origins: origins}
	src.data = nil
	return src
}
