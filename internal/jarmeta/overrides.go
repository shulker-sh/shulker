package jarmeta

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// DependencyOverrides is a loader's dependency overrides file: per mod id, the dependencies to add,
// remove or replace before the loader resolves anything. Fabric Loader's is
// config/fabric_loader_dependencies.json, FML's the dependencyOverrides table of config/fml.toml.
type DependencyOverrides map[string][]overrideEntry

// overrideEntry is one change to a mod's dependencies. An empty kind is every kind at once, which
// is how FML removes a dependency.
type overrideEntry struct {
	op   byte
	kind string
	deps map[string]Range
}

var overrideKinds = []string{"depends", "recommends", "suggests", "conflicts", "breaks"}

// ParseDependencyOverrides reads a loader's overrides file as that loader does, rejecting what it
// rejects. The file's suffix picks the format: JSON is Fabric Loader's, TOML is FML's.
func ParseDependencyOverrides(file string, data []byte) (DependencyOverrides, error) {
	if strings.HasSuffix(file, ".toml") {
		return parseFMLOverrides(data)
	}
	return parseFabricOverrides(data)
}

// parseFMLOverrides reads fml.toml's dependencyOverrides, where each mod id names "-dep" to drop
// its dependencies on dep, of every type, or "+dep" to load after dep, which changes no dependency
// and is skipped. FML logs and ignores a table of the wrong shape and an entry with neither sign,
// and throws on one that isn't a string or is empty.
func parseFMLOverrides(data []byte) (DependencyOverrides, error) {
	var cfg struct {
		DependencyOverrides any `toml:"dependencyOverrides"`
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, err
	}
	o := DependencyOverrides{}
	table, _ := cfg.DependencyOverrides.(map[string]any)
	for id, value := range table {
		list, ok := value.([]any)
		if !ok {
			list = []any{value}
		}
		removed := map[string]Range{}
		for _, entry := range list {
			s, ok := entry.(string)
			if !ok || s == "" {
				return nil, fmt.Errorf("dependencyOverrides.%s: an override must be a string like \"-dep\"", id)
			}
			if s[0] == '-' {
				removed[s[1:]] = nil
			}
		}
		if len(removed) > 0 {
			o[id] = []overrideEntry{{op: '-', deps: removed}}
		}
	}
	return o, nil
}

func parseFabricOverrides(data []byte) (DependencyOverrides, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("the root must be an object")
	}
	if key, err := dec.Token(); err != nil || key != "version" {
		return nil, errors.New(`the first key must be "version"`)
	}
	if tok, err := dec.Token(); err != nil || !isOne(tok) {
		return nil, errors.New(`unsupported "version", must be 1`)
	}
	o := DependencyOverrides{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if key != "overrides" {
			return nil, fmt.Errorf("unsupported root key %v", key)
		}
		var mods map[string]map[string]json.RawMessage
		if err := dec.Decode(&mods); err != nil {
			return nil, fmt.Errorf("overrides: %w", err)
		}
		for id, keys := range mods {
			entries, err := parseOverrideKeys(keys)
			if err != nil {
				return nil, fmt.Errorf("overrides for %s: %w", id, err)
			}
			o[id] = entries
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return o, nil
}

func isOne(tok json.Token) bool {
	n, ok := tok.(json.Number)
	if !ok {
		return false
	}
	f, err := n.Float64()
	return err == nil && f == 1
}

func parseOverrideKeys(keys map[string]json.RawMessage) ([]overrideEntry, error) {
	byKind := map[string]map[byte]map[string]Range{}
	for key, raw := range keys {
		op := byte('=')
		if strings.HasPrefix(key, "+") || strings.HasPrefix(key, "-") {
			op, key = key[0], key[1:]
		}
		if !slices.Contains(overrideKinds, key) {
			return nil, fmt.Errorf("%s is not a dependency kind, must be one of %s", key, strings.Join(overrideKinds, ", "))
		}
		var deps map[string]json.RawMessage
		if err := json.Unmarshal(raw, &deps); err != nil {
			return nil, fmt.Errorf("%s must be an object", key)
		}
		ranges := map[string]Range{}
		for id, r := range deps {
			var one string
			var many []string
			switch {
			case json.Unmarshal(r, &one) == nil:
				ranges[id] = Range{one}
			case json.Unmarshal(r, &many) == nil:
				ranges[id] = many
			default:
				return nil, fmt.Errorf("%s %s: a version range must be a string or an array of strings", key, id)
			}
		}
		if len(ranges) == 0 && op != '=' {
			continue
		}
		if byKind[key] == nil {
			byKind[key] = map[byte]map[string]Range{}
		}
		byKind[key][op] = ranges
	}
	var entries []overrideEntry
	for _, kind := range overrideKinds {
		ops := byKind[kind]
		if deps, ok := ops['=']; ok {
			entries = append(entries, overrideEntry{'=', kind, deps})
			continue
		}
		for _, op := range []byte{'-', '+'} {
			if deps, ok := ops[op]; ok {
				entries = append(entries, overrideEntry{op, kind, deps})
			}
		}
	}
	return entries, nil
}

// Apply returns info with the file's overrides for its id applied, as a copy; info itself when the
// file doesn't name it. A removal matches by id alone: neither loader compares its range.
func (o DependencyOverrides) Apply(info *Info) *Info {
	entries := o[info.ID]
	if len(entries) == 0 {
		return info
	}
	c := *info
	sets := map[string]*map[string]Range{"depends": &c.Depends, "recommends": &c.Recommends, "suggests": &c.Suggests, "conflicts": &c.Conflicts, "breaks": &c.Breaks}
	for _, set := range sets {
		*set = maps.Clone(*set)
		if *set == nil {
			*set = map[string]Range{}
		}
	}
	for _, e := range entries {
		if e.kind == "" {
			c.Optional, c.DependencySides = maps.Clone(c.Optional), maps.Clone(c.DependencySides)
			for id := range e.deps {
				for _, set := range sets {
					delete(*set, id)
				}
				delete(c.Optional, id)
				delete(c.DependencySides, id)
			}
			continue
		}
		set := *sets[e.kind]
		switch e.op {
		case '=':
			clear(set)
			maps.Copy(set, e.deps)
		case '-':
			for id := range e.deps {
				delete(set, id)
			}
		case '+':
			maps.Copy(set, e.deps)
		}
	}
	return &c
}
