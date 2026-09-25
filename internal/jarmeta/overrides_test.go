package jarmeta

import (
	"maps"
	"slices"
	"testing"
)

func TestDependencyOverrides(t *testing.T) {
	o, err := ParseDependencyOverrides([]byte(`{"version": 1, "overrides": {
		"biomeswevegone": {"-depends": {"terrablender": "IGNORED"}, "+depends": {"lithium": ["<0.12", ">=0.13"]}},
		"kleeslabs": {"breaks": {"balm-fabric": "*"}, "-breaks": {"slabbed": "*"}},
		"other": {"-depends": {"x": "*"}}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	bwg := &Info{ID: "biomeswevegone", Depends: map[string]Range{"terrablender": {">=3.0.1.7"}, "fabric": {"*"}}, Breaks: map[string]Range{}}
	got := o.Apply(bwg)
	if want := map[string]Range{"fabric": {"*"}, "lithium": {"<0.12", ">=0.13"}}; !maps.EqualFunc(got.Depends, want, slices.Equal) {
		t.Errorf("depends %v, want %v", got.Depends, want)
	}
	if _, ok := bwg.Depends["terrablender"]; !ok {
		t.Error("Apply changed the info it was given")
	}
	kleeslabs := &Info{ID: "kleeslabs", Depends: map[string]Range{}, Breaks: map[string]Range{"slabbed": {"*"}, "old": {"<1"}}}
	if want := map[string]Range{"balm-fabric": {"*"}}; !maps.EqualFunc(o.Apply(kleeslabs).Breaks, want, slices.Equal) {
		t.Errorf("breaks %v, want %v: a replace suppresses + and -", o.Apply(kleeslabs).Breaks, want)
	}
	if untouched := &(Info{ID: "sodium"}); o.Apply(untouched) != untouched {
		t.Error("a mod the file doesn't name should come back as is")
	}
}

func TestDependencyOverridesInvalid(t *testing.T) {
	for name, data := range map[string]string{
		"version not first": `{"overrides": {}, "version": 1}`,
		"version 2":         `{"version": 2}`,
		"unknown root key":  `{"version": 1, "extra": {}}`,
		"unknown kind":      `{"version": 1, "overrides": {"a": {"-requires": {"b": "*"}}}}`,
		"number range":      `{"version": 1, "overrides": {"a": {"depends": {"b": 1}}}}`,
		"not json":          `{"version": 1,`,
	} {
		if _, err := ParseDependencyOverrides([]byte(data)); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
}
