package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/schema"
)

type settingChange struct {
	Path string `json:"path"`
	From any    `json:"from,omitempty"`
	To   any    `json:"to,omitempty"`
}

func (a *app) setCmd() *cobra.Command {
	var literal bool
	cmd := &cobra.Command{
		Use:   "set <path> <value>",
		Short: "Set a field in shulker.json by its dotted path",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, doc, s, err := a.openSettings()
			if err != nil {
				return err
			}
			field, err := s.lookup(args[0])
			if err != nil {
				return err
			}
			from, _ := field.get(doc)
			var to any
			if literal {
				to, err = decodeLiteral(field.path, args[1])
			} else {
				to, err = field.coerce(args[1], from)
			}
			if err != nil {
				return err
			}
			field.put(doc, to)
			return a.saveSettings(p, doc, field, from)
		},
	}
	cmd.Flags().BoolVar(&literal, "literal", false, "parse the value as JSON, for lists, objects, or a value kept as a string")
	return cmd
}

func (a *app) unsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <path>",
		Short: "Remove a field from shulker.json by its dotted path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, doc, s, err := a.openSettings()
			if err != nil {
				return err
			}
			field, err := s.lookup(args[0])
			if err != nil {
				return err
			}
			from, ok := field.get(doc)
			if !ok {
				return a.printer.Emit(settingChange{Path: field.path}, func(l *out.Lines) {
					l.Info(field.path + " was not set")
				})
			}
			field.remove(doc)
			return a.saveSettings(p, doc, field, from)
		},
	}
}

func (a *app) getCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [path]",
		Short: "Print a field of shulker.json, or all of it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, doc, s, err := a.openSettings()
			if err != nil {
				return err
			}
			var value any = doc
			if len(args) == 1 {
				field, err := s.lookup(args[0])
				if err != nil {
					return err
				}
				var ok bool
				if value, ok = field.get(doc); !ok {
					return out.Errorf("path-not-set", "%s is not set", field.path)
				}
			}
			return a.printer.Emit(value, func(l *out.Lines) { writeValue(l.W, value) })
		},
	}
}

func writeValue(w io.Writer, value any) {
	if text, ok := value.(string); ok {
		fmt.Fprintln(w, text)
		return
	}
	data, err := fsutil.MarshalJSON(value)
	if err != nil {
		data = []byte(settingText(value))
	}
	fmt.Fprintf(w, "%s\n", bytes.TrimRight(data, "\n"))
}

func (a *app) openSettings() (*project.Project, map[string]any, *settingsSchema, error) {
	p, err := a.openProject()
	if err != nil {
		return nil, nil, nil, err
	}
	data, err := os.ReadFile(p.ManifestPath())
	if err != nil {
		return nil, nil, nil, err
	}
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, nil, nil, out.Errorf("manifest-invalid", "%s: %v", manifest.FileName, err)
	}
	s, err := loadSettingsSchema()
	if err != nil {
		return nil, nil, nil, err
	}
	return p, doc, s, nil
}

func (a *app) saveSettings(p *project.Project, doc map[string]any, field *settingField, from any) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := schema.Validate(schema.Manifest, data); err != nil {
		return schema.Invalid("manifest-invalid", manifest.FileName, data, err)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		return err
	}
	p.Manifest = m
	if err := p.SaveManifest(); err != nil {
		return err
	}
	saved, err := p.Manifest.Encode()
	if err != nil {
		return err
	}
	var written map[string]any
	dec := json.NewDecoder(bytes.NewReader(saved))
	dec.UseNumber()
	if err := dec.Decode(&written); err != nil {
		return err
	}
	change := settingChange{Path: field.path, From: from}
	change.To, _ = field.get(written)
	a.printer.LockStale = p.LockStale()
	if p.Lock != nil {
		a.warnLockDifferences(p)
	}
	return a.printer.Emit(change, func(l *out.Lines) {
		l.Items(out.Item{Kind: out.Change, Name: change.Path, From: settingText(change.From), To: settingText(change.To)})
	})
}

func decodeLiteral(path, value string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(value))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return nil, out.Errorf("usage", "--literal takes a JSON value for %s, got %q", path, value)
	}
	return v, nil
}

func settingText(v any) string {
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

type settingsSchema struct {
	root map[string]any
	defs map[string]any
}

func loadSettingsSchema() (*settingsSchema, error) {
	raw, err := schema.Raw(schema.Manifest)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	defs, _ := root["$defs"].(map[string]any)
	return &settingsSchema{root: root, defs: defs}, nil
}

func (s *settingsSchema) deref(node map[string]any) map[string]any {
	for {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node
		}
		node, _ = s.defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
	}
}

func (s *settingsSchema) types(node map[string]any) map[string]bool {
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

func (s *settingsSchema) holdsValues(node map[string]any) bool {
	types := s.types(node)
	return !types["object"] && !types["array"]
}

type settingField struct {
	path   string
	keys   []string
	schema map[string]any
	s      *settingsSchema
}

// lookup walks the schema along a dotted path. Inside a map of plain values
// (server.properties, variables, client.options, links) the rest of the path
// is one key, since those keys may contain dots themselves.
func (s *settingsSchema) lookup(path string) (*settingField, error) {
	if path == "" {
		return nil, out.Errorf("usage", "pass a path like server.eula")
	}
	segs := strings.Split(path, ".")
	node := s.root
	var keys []string
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
			return &settingField{path: path, keys: append(keys, key), schema: s.deref(values), s: s}, nil
		case isMap:
			keys, node = append(keys, seg), values
		case props != nil:
			child, ok := props[seg].(map[string]any)
			if !ok {
				return nil, pathInvalid(segs, i, slices.Sorted(maps.Keys(props)))
			}
			keys, node = append(keys, seg), child
		case s.types(node)["array"]:
			return nil, out.Errorf("path-invalid", "%s is a list; set the whole list with --literal", parent)
		default:
			return nil, out.Errorf("path-invalid", "%s is a single value, so it has no %q", parent, seg)
		}
	}
	return &settingField{path: path, keys: keys, schema: s.deref(node), s: s}, nil
}

func pathInvalid(segs []string, i int, allowed []string) error {
	where := manifest.FileName
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

func (f *settingField) get(doc map[string]any) (any, bool) {
	var cur any = doc
	for _, key := range f.keys {
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

func (f *settingField) put(doc map[string]any, value any) {
	m := doc
	for _, key := range f.keys[:len(f.keys)-1] {
		next, ok := m[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[key] = next
		}
		m = next
	}
	m[f.keys[len(f.keys)-1]] = value
}

func (f *settingField) remove(doc map[string]any) {
	m := doc
	for _, key := range f.keys[:len(f.keys)-1] {
		next, ok := m[key].(map[string]any)
		if !ok {
			return
		}
		m = next
	}
	delete(m, f.keys[len(f.keys)-1])
}

func (f *settingField) coerce(value string, current any) (any, error) {
	types := f.s.types(f.schema)
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
		kind := "an object"
		if types["array"] {
			kind = "a list"
		}
		return nil, out.Errorf("usage", "%s takes %s; pass it as JSON with --literal", f.path, kind)
	}
	var want []string
	if types["boolean"] {
		want = append(want, "true or false")
	}
	if types["number"] {
		want = append(want, "a number")
	} else if types["integer"] {
		want = append(want, "a whole number")
	}
	return nil, out.Errorf("usage", "%s takes %s, not %q", f.path, strings.Join(want, " or "), value)
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

func (f *settingField) addPlayer(list []any, value string) ([]any, error) {
	name, uuid := f.s.splitPlayer(value)
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
			return nil, out.Errorf("usage", "%s matches two entries in %s", value, f.path)
		case sameName && uuid != "" && listedUUID != "" && !sameUUID:
			return nil, out.Errorf("usage", "%s is listed in %s with uuid %s", listedName, f.path, listedUUID)
		case sameUUID && name != "" && listedName != "" && !sameName:
			return nil, out.Errorf("usage", "%s is listed in %s as %s", uuid, f.path, listedName)
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

func (s *settingsSchema) splitPlayer(value string) (name, uuid string) {
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
