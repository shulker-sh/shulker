package build

import (
	"bytes"
	"maps"
	"regexp"
	"strings"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
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

func render(name string, data []byte, vars map[string]string) ([]byte, error) {
	var firstErr error
	rendered := varRe.ReplaceAllFunc(data, func(match []byte) []byte {
		key := string(varRe.FindSubmatch(match)[1])
		val, ok := vars[key]
		if !ok {
			if firstErr == nil {
				line := 1 + bytes.Count(data[:bytes.Index(data, match)], []byte("\n"))
				firstErr = out.Errorf("unset-variable", "%s:%d: variable ${%s} is not set", name, line, key)
			}
			return match
		}
		return []byte(val)
	})
	return rendered, firstErr
}
