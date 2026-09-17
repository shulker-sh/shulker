package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type docsPage struct {
	kind        Kind
	name        string
	intro       string
	description string
}

var docsPages = []docsPage{
	{Manifest, "manifest.md", "The project manifest. Hand-edited, committed, and read by every command.", "Every field in shulker.json, the project manifest, generated from its JSON Schema."},
	{Instance, "instance.md", "One instance's own file. Hand-edit it to change what shulker does with that instance, then sync.", "Every field in .shulker/instance.json, which records what an instance syncs from and how shulker sets it up, generated from its JSON Schema."},
}

var (
	newlines  = regexp.MustCompile(`\n+`)
	blankRuns = regexp.MustCompile(`\n{3,}`)
)

// object keeps its keys in document order, because the pages list
// properties and definitions in the order the schema declares them.
type object struct {
	keys   []string
	values map[string]any
}

func (o *object) get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.values[key]
	return v, ok
}

func (o *object) field(key string) string {
	v, _ := o.get(key)
	if v == nil {
		return ""
	}
	return str(v)
}

func (o *object) truthy(key string) bool {
	v, _ := o.get(key)
	return truthy(v)
}

func renderDocsPage(page docsPage) ([]byte, error) {
	raw, err := files.ReadFile(string(page.kind))
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	doc, err := decodeOrdered(dec)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", page.kind, err)
	}
	root, ok := doc.(*object)
	if !ok {
		return nil, fmt.Errorf("%s is not an object", page.kind)
	}
	id, err := url.Parse(root.field("$id"))
	if err != nil {
		return nil, fmt.Errorf("%s $id: %w", page.kind, err)
	}
	lines := []string{
		"---",
		"description: " + stringify(page.description),
		"editLink: false",
		"---",
		"",
		"# " + root.field("title"),
		"",
		page.intro,
		"",
		escape(root.field("description")),
		"",
		"Schema: [" + root.field("$id") + "](" + id.EscapedPath() + ")",
		"",
		"## Properties",
		"",
		"Required properties are marked with *.",
		"",
	}
	lines = append(lines, describe(root, "")...)
	lines = append(lines, "## Definitions", "")
	defs, _ := root.get("$defs")
	if defs, ok := defs.(*object); ok {
		for _, name := range defs.keys {
			def, _ := defs.values[name].(*object)
			lines = append(lines, "### "+name, "")
			lines = append(lines, describe(def, def.field("description"))...)
		}
	}
	return []byte(blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")), nil
}

func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		o := &object{values: map[string]any{}}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			name := key.(string)
			if _, seen := o.values[name]; !seen {
				o.keys = append(o.keys, name)
			}
			o.values[name] = v
		}
		_, err := dec.Token()
		return o, err
	case json.Delim('['):
		items := []any{}
		for dec.More() {
			v, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		_, err := dec.Token()
		return items, err
	}
	return tok, nil
}

func describe(s *object, description string) []string {
	var lines []string
	if description != "" {
		lines = append(lines, escape(description), "")
	}
	if table := propertyTable(s); table != "" {
		lines = append(lines, table, "")
		if ap, ok := s.get("additionalProperties"); ok && ap == false {
			lines = append(lines, "No other properties are allowed.", "")
		}
	} else if t := typeOf(s); t != "" {
		if c := constraints(s); len(c) > 0 {
			t += ". " + strings.Join(c, ", ")
		}
		lines = append(lines, "Type: "+t, "")
	}
	return lines
}

func propertyTable(s *object) string {
	v, _ := s.get("properties")
	props, _ := v.(*object)
	if props == nil || len(props.keys) == 0 {
		return ""
	}
	required := map[string]bool{}
	if v, ok := s.get("required"); ok {
		for _, name := range list(v) {
			required[str(name)] = true
		}
	}
	conditional := conditionalTypes(s)
	rows := []string{"| Property | Type | Description |", "| --- | --- | --- |"}
	for _, name := range props.keys {
		p, _ := props.values[name].(*object)
		t := typeOf(p)
		if t == "" {
			t = strings.Join(conditional[name], " \\| ")
		}
		var cells []string
		for _, c := range constraints(p) {
			cells = append(cells, cell(c))
		}
		desc := cell(p.field("description"))
		if joined := strings.Join(cells, ", "); desc == "" {
			desc = joined
		} else if joined != "" {
			desc += "<br>" + joined
		}
		mark := ""
		if required[name] {
			mark = " *"
		}
		rows = append(rows, "| "+code(name)+mark+" | "+t+" | "+desc+" |")
	}
	return strings.Join(rows, "\n")
}

func conditionalTypes(s *object) map[string][]string {
	found := map[string][]string{}
	allOf, _ := s.get("allOf")
	for _, branch := range list(allOf) {
		b, _ := branch.(*object)
		then, _ := b.get("then")
		t, _ := then.(*object)
		props, _ := t.get("properties")
		p, _ := props.(*object)
		if p == nil {
			continue
		}
		for _, name := range p.keys {
			t := typeOf(p.values[name])
			if t != "" && !contains(found[name], t) {
				found[name] = append(found[name], t)
			}
		}
	}
	return found
}

func typeOf(v any) string {
	s, ok := v.(*object)
	if !ok || s == nil {
		return ""
	}
	if s.truthy("$ref") {
		return anchor(refName(s.field("$ref")))
	}
	if c, ok := s.get("const"); ok {
		return code(stringify(c))
	}
	if e, _ := s.get("enum"); truthy(e) {
		var parts []string
		for _, item := range list(e) {
			parts = append(parts, code(stringify(item)))
		}
		return strings.Join(parts, " \\| ")
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if branches, _ := s.get(key); truthy(branches) {
			return joinTypes(branches, " \\| ")
		}
	}
	if branches, _ := s.get("allOf"); truthy(branches) {
		return joinTypes(branches, " & ")
	}
	t, _ := s.get("type")
	switch {
	case t == "array":
		items, _ := s.get("items")
		inner := typeOf(items)
		if inner == "" {
			inner = "any"
		}
		return inner + "[]"
	case t == "object":
		if s.truthy("properties") && !s.truthy("additionalProperties") {
			return "object"
		}
		ap, _ := s.get("additionalProperties")
		if inner := typeOf(ap); inner != "" {
			return "map of " + inner
		}
		return "object"
	}
	if types, ok := t.([]any); ok {
		var parts []string
		for _, item := range types {
			parts = append(parts, code(str(item)))
		}
		return strings.Join(parts, " \\| ")
	}
	if truthy(t) {
		return code(str(t))
	}
	return ""
}

func joinTypes(branches any, sep string) string {
	var parts []string
	for _, b := range list(branches) {
		if t := typeOf(b); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, sep)
}

func constraints(s *object) []string {
	var out []string
	if s.truthy("format") {
		out = append(out, "format "+code(s.field("format")))
	}
	if s.truthy("pattern") {
		out = append(out, "pattern "+code(s.field("pattern")))
	}
	for _, c := range []struct{ key, label string }{
		{"minLength", "min length"},
		{"minimum", "min"},
		{"maximum", "max"},
		{"minItems", "min items"},
		{"minProperties", "min properties"},
	} {
		if v, ok := s.get(c.key); ok {
			out = append(out, c.label+" "+str(v))
		}
	}
	if s.truthy("uniqueItems") {
		out = append(out, "unique items")
	}
	names, _ := s.get("propertyNames")
	if pn, _ := names.(*object); pn != nil {
		if pn.truthy("pattern") {
			out = append(out, "keys match "+code(pn.field("pattern")))
		}
		if pn.truthy("$ref") {
			out = append(out, "keys are "+anchor(refName(pn.field("$ref"))))
		}
	}
	if d, ok := s.get("default"); ok {
		out = append(out, "default "+code(stringify(d)))
	}
	return out
}

func code(s string) string { return "`" + strings.ReplaceAll(s, "`", "\\`") + "`" }

func escape(s string) string {
	return strings.NewReplacer("<", "&lt;", ">", "&gt;", "{{", "&#123;&#123;").Replace(s)
}

func cell(s string) string {
	return strings.TrimSpace(newlines.ReplaceAllString(strings.ReplaceAll(escape(s), "|", "\\|"), " "))
}

func refName(ref string) string { return strings.TrimPrefix(ref, "#/$defs/") }

func anchor(name string) string { return "[" + code(name) + "](#" + strings.ToLower(name) + ")" }

func list(v any) []any {
	items, _ := v.([]any)
	return items
}

func contains(items []string, s string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}

func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case json.Number:
		f, err := v.Float64()
		return err != nil || f != 0
	}
	return true
}

func str(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case json.Number:
		return number(v)
	case bool:
		return strconv.FormatBool(v)
	}
	return fmt.Sprint(v)
}

func number(n json.Number) string {
	f, err := n.Float64()
	if err != nil {
		return n.String()
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func stringify(v any) string {
	switch v := v.(type) {
	case *object:
		parts := make([]string, len(v.keys))
		for i, key := range v.keys {
			parts[i] = stringify(key) + ":" + stringify(v.values[key])
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = stringify(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case json.Number:
		return number(v)
	case nil:
		return "null"
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}
