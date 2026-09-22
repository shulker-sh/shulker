package build

import (
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/near"
	"shulker.sh/shulker/internal/out"
)

var varRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\}`)

// templateVars are the side's variables from m with the built-ins laid over them. Built-in names
// are dotted, which a manifest's variable keys can't be, so neither can shadow the other; a
// built-in whose value is missing stays unset.
func templateVars(m *manifest.Manifest, l *lock.Lock, side string) map[string]string {
	vars := m.SideVariables(side).Text()
	builtins := map[string]string{"pack.name": m.Name, "pack.version": m.Version}
	if l != nil {
		builtins["minecraft.version"] = l.Minecraft
		builtins["loader.type"] = l.Loader.Type
		builtins["loader.version"] = l.Loader.Version
	}
	maps.DeleteFunc(builtins, func(_, v string) bool { return v == "" })
	maps.Copy(vars, builtins)
	return vars
}

// pulledTemplateVars are a pulled pack's variables with the project's laid over them, all but
// the pack built-ins, which name the pulled pack itself.
func pulledTemplateVars(m *manifest.Manifest, l *lock.Lock, side string, project map[string]string) map[string]string {
	vars := templateVars(m, l, side)
	for k, v := range project {
		if !strings.HasPrefix(k, "pack.") {
			vars[k] = v
		}
	}
	return vars
}

// render replaces each ${name} in data with its variable. pack is the pulled pack the template
// comes from, empty for the project's own.
func render(name, pack string, data []byte, vars map[string]string) ([]byte, error) {
	var firstErr error
	rendered := varRe.ReplaceAllFunc(data, func(match []byte) []byte {
		key := string(varRe.FindSubmatch(match)[1])
		val, ok := vars[key]
		if !ok {
			if firstErr == nil {
				line := 1 + bytes.Count(data[:bytes.Index(data, match)], []byte("\n"))
				firstErr = unsetVariable(fmt.Sprintf("%s:%d", name, line), pack, key, vars)
			}
			return match
		}
		return []byte(val)
	})
	return rendered, firstErr
}

func unsetVariable(at, pack, key string, vars map[string]string) *out.Error {
	e := out.Errorf("unset-variable", "%s: variable ${%s} is not set", at, key)
	names := slices.Sorted(maps.Keys(vars))
	if len(near.Closest(key, names, 1)) > 0 {
		e.Given, e.Candidates = key, names
	}
	switch {
	case key == "pack.version" && pack != "":
		e.Help = fmt.Sprintf(`set "version" in %s's shulker.json`, pack)
	case key == "pack.version":
		e.Help = "run `shulker set version <version>`"
	case key == "loader.type" || key == "loader.version":
		e.Help = "the lock has no loader: set \"loader\" in shulker.json, then run `shulker lock`"
	case key == "minecraft.version":
		e.Help = "run `shulker lock`"
	case !strings.Contains(key, "."):
		e.Help = fmt.Sprintf("run `shulker set variables.%s <value>`", key)
	}
	return e
}
