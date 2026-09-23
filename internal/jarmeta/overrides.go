package jarmeta

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// DependencyOverrides is Fabric Loader's config/fabric_loader_dependencies.json: per mod id, the
// dependencies to add, remove or replace before the loader resolves anything.
type DependencyOverrides map[string][]overrideEntry

type overrideEntry struct {
	op   byte
	kind string
	deps map[string]string
}

var overrideKinds = []string{"depends", "recommends", "suggests", "conflicts", "breaks"}

// ParseDependencyOverrides reads the file as Fabric Loader does, rejecting what it rejects.
func ParseDependencyOverrides(data []byte) (DependencyOverrides, error) {
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
	byKind := map[string]map[byte]map[string]string{}
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
		ranges := map[string]string{}
		for id, r := range deps {
			var one string
			var many []string
			switch {
			case json.Unmarshal(r, &one) == nil:
				ranges[id] = one
			case json.Unmarshal(r, &many) == nil:
				ranges[id] = strings.Join(many, " || ")
			default:
				return nil, fmt.Errorf("%s %s: a version range must be a string or an array of strings", key, id)
			}
		}
		if len(ranges) == 0 && op != '=' {
			continue
		}
		if byKind[key] == nil {
			byKind[key] = map[byte]map[string]string{}
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
// file doesn't name it. A removal matches by id alone: Fabric never compares its range.
func (o DependencyOverrides) Apply(info *Info) *Info {
	entries := o[info.ID]
	if len(entries) == 0 {
		return info
	}
	c := *info
	sets := map[string]*map[string]string{"depends": &c.Depends, "recommends": &c.Recommends, "suggests": &c.Suggests, "conflicts": &c.Conflicts, "breaks": &c.Breaks}
	for _, set := range sets {
		*set = maps.Clone(*set)
		if *set == nil {
			*set = map[string]string{}
		}
	}
	for _, e := range entries {
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
