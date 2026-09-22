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
	props, err := describe(root, "")
	if err != nil {
		return nil, err
	}
	lines = append(lines, props...)
	lines = append(lines, "## Definitions", "")
	defs, _ := root.get("$defs")
	if defs, ok := defs.(*object); ok {
		for _, name := range defs.keys {
			def, _ := defs.values[name].(*object)
			lines = append(lines, "### "+name, "")
			described, err := describe(def, def.field("description"))
			if err != nil {
				return nil, fmt.Errorf("%s $defs/%s: %w", page.kind, name, err)
			}
			lines = append(lines, described...)
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

func describe(s *object, description string) ([]string, error) {
	var lines []string
	if description != "" {
		lines = append(lines, escape(description), "")
	}
	v, _ := s.get("properties")
	if props, _ := v.(*object); props == nil || len(props.keys) == 0 {
		if t := typeOf(s); t != "" {
			if c := constraints(s); len(c) > 0 {
				t += ". " + strings.Join(c, ", ")
			}
			lines = append(lines, "Type: "+t, "")
		}
		return lines, nil
	}
	var t table
	if err := t.add(s, ""); err != nil {
		return nil, err
	}
	lines = append(lines, "| Property | Type | Description |", "| --- | --- | --- |")
	lines = append(lines, t.rows...)
	lines = append(lines, "")
	for _, rule := range t.rules {
		lines = append(lines, rule, "")
	}
	if ap, ok := s.get("additionalProperties"); ok && ap == false {
		lines = append(lines, "No other properties are allowed.", "")
	}
	return lines, nil
}

// table flattens nested objects into dotted rows, so an object's rules
// name their keys with the same prefix the rows do.
type table struct {
	rows  []string
	rules []string
}

func (t *table) add(s *object, prefix string) error {
	v, _ := s.get("properties")
	props, _ := v.(*object)
	if props == nil {
		return nil
	}
	required := map[string]bool{}
	if v, ok := s.get("required"); ok {
		for _, name := range list(v) {
			required[str(name)] = true
		}
	}
	conditional := conditionalTypes(s)
	for _, name := range props.keys {
		p, _ := props.values[name].(*object)
		t.rows = append(t.rows, propertyRow(p, prefix+name, required[name], conditional[name]))
		for _, nested := range nestedObjects(p, prefix+name) {
			if err := t.add(nested.schema, nested.prefix); err != nil {
				return err
			}
		}
	}
	rules, err := objectRules(s, prefix)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSuffix(prefix, "."), err)
	}
	t.rules = append(t.rules, rules...)
	return nil
}

func propertyRow(p *object, name string, required bool, conditional []string) string {
	t := typeOf(p)
	if t == "" {
		t = strings.Join(conditional, " \\| ")
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
	if required {
		mark = " *"
	}
	return "| " + code(name) + mark + " | " + t + " | " + desc + " |"
}

type nestedObject struct {
	schema *object
	prefix string
}

func nestedObjects(p *object, name string) []nestedObject {
	if p == nil || p.truthy("$ref") {
		return nil
	}
	if p.truthy("properties") {
		return []nestedObject{{p, name + "."}}
	}
	if t, _ := p.get("type"); t == "array" {
		items, _ := p.get("items")
		i, _ := items.(*object)
		return nestedObjects(i, name+"[]")
	}
	var found []nestedObject
	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		branches, _ := p.get(key)
		for _, b := range list(branches) {
			b, _ := b.(*object)
			found = append(found, nestedObjects(b, name)...)
		}
	}
	return found
}

func objectRules(s *object, prefix string) ([]string, error) {
	var rules []string
	for _, key := range []string{"if", "then", "else", "not", "dependentSchemas"} {
		if _, ok := s.get(key); ok {
			return nil, fmt.Errorf("the docs renderer cannot state %s", key)
		}
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		branches, ok := s.get(key)
		if !ok {
			continue
		}
		var options []string
		for _, b := range list(branches) {
			names, ok := requiredOnly(b)
			if !ok {
				return nil, fmt.Errorf("the docs renderer cannot state an %s branch other than required", key)
			}
			options = append(options, join(names, prefix, "and"))
		}
		quantity := "At least one"
		if key == "oneOf" {
			quantity = "Exactly one"
		}
		rules = append(rules, quantity+" of "+joinWords(options, "or")+" is required.")
	}
	dependent, _ := s.get("dependentRequired")
	if d, _ := dependent.(*object); d != nil {
		var order []string
		groups := map[string][]string{}
		for _, key := range d.keys {
			target := join(strs(d.values[key]), prefix, "and")
			if _, seen := groups[target]; !seen {
				order = append(order, target)
			}
			groups[target] = append(groups[target], key)
		}
		for _, target := range order {
			verb := " require "
			if len(groups[target]) == 1 {
				verb = " requires "
			}
			rules = append(rules, join(groups[target], prefix, "and")+verb+target+".")
		}
	}
	allOf, _ := s.get("allOf")
	for _, branch := range list(allOf) {
		b, _ := branch.(*object)
		if b == nil || !b.truthy("if") {
			if b != nil && (b.truthy("$ref") || b.truthy("properties")) {
				continue
			}
			return nil, fmt.Errorf("the docs renderer cannot state an allOf branch without if")
		}
		rule, err := conditionalRule(b, prefix)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func conditionalRule(b *object, prefix string) (string, error) {
	cond, _ := b.get("if")
	when, ok := condition(cond, prefix)
	if !ok {
		return "", fmt.Errorf("the docs renderer cannot state an if other than required")
	}
	if _, ok := b.get("else"); ok {
		return "", fmt.Errorf("the docs renderer cannot state else")
	}
	then, _ := b.get("then")
	t, _ := then.(*object)
	if t == nil {
		return "", fmt.Errorf("the docs renderer cannot state an if without then")
	}
	var clauses []string
	for _, key := range t.keys {
		switch key {
		case "required":
			names := strs(t.values[key])
			clauses = append(clauses, join(names, prefix, "and")+plural(names, " is", " are")+" required")
		case "properties":
			props, _ := t.values[key].(*object)
			for _, name := range props.keys {
				clauses = append(clauses, code(prefix+name)+" must be "+typeOf(props.values[name]))
			}
		case "not":
			not, _ := t.values[key].(*object)
			anyOf, _ := not.get("anyOf")
			var banned []string
			for _, branch := range list(anyOf) {
				names, ok := requiredOnly(branch)
				if !ok || len(names) != 1 {
					return "", fmt.Errorf("the docs renderer cannot state this not")
				}
				banned = append(banned, names[0])
			}
			if len(banned) == 0 || len(not.keys) != 1 {
				return "", fmt.Errorf("the docs renderer cannot state this not")
			}
			clauses = append(clauses, join(banned, prefix, "and")+plural(banned, " is", " are")+" not allowed")
		case "anyOf":
			var options []string
			for _, branch := range list(t.values[key]) {
				names, ok := requiredOnly(branch)
				if !ok || len(names) != 1 {
					return "", fmt.Errorf("the docs renderer cannot state this anyOf")
				}
				options = append(options, names[0])
			}
			clauses = append(clauses, join(options, prefix, "or")+" is required")
		default:
			return "", fmt.Errorf("the docs renderer cannot state then.%s", key)
		}
	}
	return "When " + when + ", " + strings.Join(clauses, ", and ") + ".", nil
}

// condition states an if that requires keys, some of which it may also hold to a const: "`file` is
// set and `type` is `\"modpack\"`".
func condition(v any, prefix string) (string, bool) {
	b, _ := v.(*object)
	if b == nil {
		return "", false
	}
	required, _ := b.get("required")
	names := strs(required)
	if len(names) == 0 {
		return "", false
	}
	valued := map[string]string{}
	for _, key := range b.keys {
		switch key {
		case "required":
		case "properties":
			props, _ := b.values[key].(*object)
			for _, name := range props.keys {
				p, _ := props.values[name].(*object)
				c, ok := p.get("const")
				if !ok || len(p.keys) != 1 || !contains(names, name) {
					return "", false
				}
				valued[name] = code(stringify(c))
			}
		default:
			return "", false
		}
	}
	var set, parts []string
	for _, name := range names {
		if value, ok := valued[name]; ok {
			parts = append(parts, code(prefix+name)+" is "+value)
		} else {
			set = append(set, name)
		}
	}
	if len(set) > 0 {
		parts = append([]string{join(set, prefix, "and") + plural(set, " is", " are") + " set"}, parts...)
	}
	return strings.Join(parts, " and "), true
}

func requiredOnly(v any) ([]string, bool) {
	b, _ := v.(*object)
	if b == nil || len(b.keys) != 1 || b.keys[0] != "required" {
		return nil, false
	}
	return strs(b.values["required"]), true
}

func strs(v any) []string {
	var out []string
	for _, item := range list(v) {
		out = append(out, str(item))
	}
	return out
}

func plural(items []string, one, many string) string {
	if len(items) == 1 {
		return one
	}
	return many
}

func join(names []string, prefix, conj string) string {
	coded := make([]string, len(names))
	for i, name := range names {
		coded[i] = code(prefix + name)
	}
	return joinWords(coded, conj)
}

func joinWords(words []string, conj string) string {
	if len(words) <= 1 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " " + conj + " " + words[len(words)-1]
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
