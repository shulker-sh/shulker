package schema

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/out"
)

// FieldSet is the schema a dotted path is looked up in, and where is the file it describes, for
// the error naming a path it lacks. base is where in the file the paths
// start: a path is typed relative to it, and every field it resolves to is written beneath it.
type FieldSet struct {
	root  map[string]any
	defs  map[string]any
	base  []string
	where string
}

func Fields(kind Kind, where string, base ...string) (*FieldSet, error) {
	raw, err := Raw(kind)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	defs, _ := root["$defs"].(map[string]any)
	s := &FieldSet{root: root, defs: defs, base: base, where: where}
	for _, key := range base {
		props, _ := s.deref(s.root)["properties"].(map[string]any)
		s.root, _ = props[key].(map[string]any)
	}
	return s, nil
}

func (s *FieldSet) deref(node map[string]any) map[string]any {
	for {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node
		}
		node, _ = s.defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
	}
}

func (s *FieldSet) types(node map[string]any) map[string]bool {
	node = s.deref(node)
	types := map[string]bool{}
	switch t := node["type"].(type) {
	case string:
		types[t] = true
	case []any:
		for _, name := range t {
			if name, ok := name.(string); ok {
				types[name] = true
			}
		}
	}
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		branches, _ := node[key].([]any)
		for _, branch := range branches {
			if branch, ok := branch.(map[string]any); ok {
				maps.Copy(types, s.types(branch))
			}
		}
	}
	return types
}

func (s *FieldSet) holdsValues(node map[string]any) bool {
	types := s.types(node)
	return !types["object"] && !types["array"]
}

// Field is one dotted path resolved against a FieldSet: where it sits in the document and what
// its schema allows there.
type Field struct {
	// Path is the dotted path as it was typed, and Keys the keys it resolves to in the document.
	Path   string
	Keys   []string
	schema map[string]any
	set    *FieldSet
}

// Lookup walks the schema along a dotted path. Inside a map of plain values
// (server.properties, variables, client.options, links) the rest of the path
// is one key, since those keys may contain dots themselves.
func (s *FieldSet) Lookup(path string) (*Field, error) {
	if path == "" {
		return nil, out.Errorf("usage", "pass a path like server.eula")
	}
	segs := strings.Split(path, ".")
	node := s.root
	keys := slices.Clone(s.base)
	for i, seg := range segs {
		node = s.deref(node)
		parent := strings.Join(segs[:i], ".")
		if seg == "" {
			return nil, out.Errorf("path-invalid", "%q has an empty segment", path)
		}
		props, _ := node["properties"].(map[string]any)
		values, isMap := node["additionalProperties"].(map[string]any)
		switch {
		case isMap && s.holdsValues(values):
			key := strings.Join(segs[i:], ".")
			if known, ok := props[key].(map[string]any); ok {
				values = known
			}
			return &Field{Path: path, Keys: append(keys, key), schema: s.deref(values), set: s}, nil
		case isMap:
			keys, node = append(keys, seg), values
		case props != nil:
			child, ok := props[seg].(map[string]any)
			if !ok {
				return nil, pathInvalid(s.where, segs, i, slices.Sorted(maps.Keys(props)))
			}
			keys, node = append(keys, seg), child
		case s.types(node)["array"]:
			e := out.Errorf("path-invalid", "%s is a list", parent)
			e.Help = "set the whole list with --literal"
			return nil, e
		default:
			return nil, out.Errorf("path-invalid", "%s is a single value, so it has no %q", parent, seg)
		}
	}
	return &Field{Path: path, Keys: keys, schema: s.deref(node), set: s}, nil
}

func pathInvalid(where string, segs []string, i int, allowed []string) error {
	if parent := strings.Join(segs[:i], "."); parent != "" {
		where = parent
	}
	e := out.Errorf("path-invalid", "%s has no %q", where, segs[i])
	e.Candidates, e.Given = allowed, strings.Join(segs, ".")
	for _, key := range allowed {
		e.Pass = append(e.Pass, strings.Join(slices.Concat(segs[:i], []string{key}, segs[i+1:]), "."))
	}
	return e
}

// Get is the field's value in doc, if it is set.
func (f *Field) Get(doc map[string]any) (any, bool) {
	var cur any = doc
	for _, key := range f.Keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[key]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// Put writes value at the field, creating the objects on the way to it.
func (f *Field) Put(doc map[string]any, value any) {
	m := doc
	for _, key := range f.Keys[:len(f.Keys)-1] {
		next, ok := m[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[key] = next
		}
		m = next
	}
	m[f.Keys[len(f.Keys)-1]] = value
}

// Remove deletes the field from doc, leaving the objects above it.
func (f *Field) Remove(doc map[string]any) {
	m := doc
	for _, key := range f.Keys[:len(f.Keys)-1] {
		next, ok := m[key].(map[string]any)
		if !ok {
			return
		}
		m = next
	}
	delete(m, f.Keys[len(f.Keys)-1])
}

// RemoveEmptied is Remove, then drops each object the removal left empty. config.json holds only
// optional objects; a manifest's may be required, so its unset keeps them.
func (f *Field) RemoveEmptied(doc map[string]any) {
	f.Remove(doc)
	for depth := len(f.Keys) - 1; depth > 0; depth-- {
		parent := &Field{Keys: f.Keys[:depth]}
		v, _ := parent.Get(doc)
		if m, ok := v.(map[string]any); !ok || len(m) > 0 {
			return
		}
		parent.Remove(doc)
	}
}

// Coerce reads typed text by the field's types: a boolean, a number, a string, or an entry added
// to a player list on top of current.
func (f *Field) Coerce(value string, current any) (any, error) {
	types := f.set.types(f.schema)
	if items, ok := f.schema["items"].(map[string]any); ok && types["array"] && isPlayerList(items) {
		list, _ := current.([]any)
		return f.addPlayer(list, value)
	}
	if types["boolean"] && (value == "true" || value == "false") {
		return value == "true", nil
	}
	if isJSONNumber(value) {
		whole := !strings.ContainsAny(value, ".eE")
		if types["number"] || types["integer"] && whole {
			return json.Number(value), nil
		}
	}
	if types["string"] || len(types) == 0 {
		return value, nil
	}
	if types["array"] || types["object"] {
		e := out.Errorf("usage", "%s takes %s", f.Path, f.Kind())
		e.Help = "pass it as JSON with --literal"
		return nil, e
	}
	return nil, out.Errorf("usage", "%s takes %s, not %q", f.Path, f.Kind(), value)
}

// Kind says what the field holds, for an error refusing a value it can't.
func (f *Field) Kind() string {
	types := f.set.types(f.schema)
	switch {
	case types["array"]:
		return "a list"
	case types["object"]:
		return "an object"
	}
	var want []string
	if types["string"] {
		want = append(want, "a string")
	}
	if types["boolean"] {
		want = append(want, "true or false")
	}
	if types["number"] {
		want = append(want, "a number")
	} else if types["integer"] {
		want = append(want, "a whole number")
	}
	return strings.Join(want, " or ")
}

func isPlayerList(items map[string]any) bool {
	if items["$ref"] == "#/$defs/player" {
		return true
	}
	branches, _ := items["allOf"].([]any)
	for _, branch := range branches {
		if b, _ := branch.(map[string]any); b["$ref"] == "#/$defs/player" {
			return true
		}
	}
	return false
}

func (f *Field) addPlayer(list []any, value string) ([]any, error) {
	name, uuid := f.set.splitPlayer(value)
	found := -1
	var match map[string]any
	for i, entry := range list {
		e, _ := entry.(map[string]any)
		listedName, _ := e["name"].(string)
		listedUUID, _ := e["uuid"].(string)
		sameName := name != "" && strings.EqualFold(listedName, name)
		sameUUID := uuid != "" && listedUUID == uuid
		if !sameName && !sameUUID {
			continue
		}
		switch {
		case found >= 0:
			return nil, out.Errorf("usage", "%s matches two entries in %s", value, f.Path)
		case sameName && uuid != "" && listedUUID != "" && !sameUUID:
			return nil, out.Errorf("usage", "%s is listed in %s with uuid %s", listedName, f.Path, listedUUID)
		case sameUUID && name != "" && listedName != "" && !sameName:
			return nil, out.Errorf("usage", "%s is listed in %s as %s", uuid, f.Path, listedName)
		}
		found, match = i, e
	}
	if found < 0 {
		entry := map[string]any{}
		if name != "" {
			entry["name"] = name
		}
		if uuid != "" {
			entry["uuid"] = uuid
		}
		return append(slices.Clone(list), entry), nil
	}
	filled := maps.Clone(match)
	if _, ok := filled["name"]; !ok && name != "" {
		filled["name"] = name
	}
	if _, ok := filled["uuid"]; !ok && uuid != "" {
		filled["uuid"] = uuid
	}
	updated := slices.Clone(list)
	updated[found] = filled
	return updated, nil
}

func (s *FieldSet) splitPlayer(value string) (name, uuid string) {
	if n, u, ok := strings.Cut(value, ":"); ok {
		return n, u
	}
	player, _ := s.defs["player"].(map[string]any)
	props, _ := player["properties"].(map[string]any)
	uuidSchema, _ := props["uuid"].(map[string]any)
	if pattern, ok := uuidSchema["pattern"].(string); ok && regexp.MustCompile(pattern).MatchString(value) {
		return "", value
	}
	return value, ""
}

func isJSONNumber(s string) bool {
	return s != "" && (s[0] == '-' || s[0] >= '0' && s[0] <= '9') && json.Valid([]byte(s))
}

// Names is every key the set's properties list, sorted.
func (s *FieldSet) Names() []string {
	props, _ := s.deref(s.root)["properties"].(map[string]any)
	return slices.Sorted(maps.Keys(props))
}

// Default is the value the schema gives the field when the document has none, if it names one.
func (f *Field) Default() (any, bool) {
	v, ok := f.schema["default"]
	return v, ok
}
