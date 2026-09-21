package jarmeta

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"strings"

	"github.com/BurntSushi/toml"
)

type modsTOML struct {
	Mods []struct {
		ModID   string `toml:"modId"`
		Version string `toml:"version"`
	} `toml:"mods"`
	Dependencies map[string][]struct {
		ModID        string `toml:"modId"`
		Type         string `toml:"type"`
		Mandatory    any    `toml:"mandatory"`
		VersionRange string `toml:"versionRange"`
	} `toml:"dependencies"`
}

// readModsTOML reads NeoForge's neoforge.mods.toml and Forge's mods.toml. Both leave the side to
// the provider, and FML reads dependencies only from tables keyed by one of the jar's own mods.
func readModsTOML(zr *zip.Reader, f *zip.File, files []string) (*Info, error) {
	data, err := readFile(f)
	if err != nil {
		return nil, err
	}
	var raw modsTOML
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, err
	}
	if len(raw.Mods) == 0 || raw.Mods[0].ModID == "" {
		return nil, errors.New("missing modId")
	}
	jarVersion := manifestAttribute(zr, "Implementation-Version")
	if jarVersion == "" {
		jarVersion = "0.0NONE"
	}
	version := func(v string) string {
		if v == "" {
			return "1"
		}
		return strings.ReplaceAll(v, "${file.jarVersion}", jarVersion)
	}
	loaderName := "forge"
	if f.Name == "META-INF/neoforge.mods.toml" {
		loaderName = "neoforge"
	}
	info := &Info{
		ID:              raw.Mods[0].ModID,
		Version:         version(raw.Mods[0].Version),
		Loader:          loaderName,
		Side:            "both",
		Depends:         map[string]string{},
		Breaks:          map[string]string{},
		Conflicts:       map[string]string{},
		Recommends:      map[string]string{},
		Suggests:        map[string]string{},
		Optional:        map[string]string{},
		Provides:        map[string]string{},
		UsesMavenRanges: true,
	}
	own := map[string]bool{}
	for _, m := range raw.Mods {
		own[m.ModID] = true
		if m.ModID != info.ID {
			info.Provides[m.ModID] = version(m.Version)
		}
	}
	for _, m := range raw.Mods {
		for _, dep := range raw.Dependencies[m.ModID] {
			if dep.ModID == "" || own[dep.ModID] {
				continue
			}
			rng := strings.TrimSpace(dep.VersionRange)
			if rng == "" {
				rng = "*"
			}
			switch dependencyType(dep.Type, dep.Mandatory) {
			case "required":
				info.Depends[dep.ModID] = rng
			case "optional":
				info.Optional[dep.ModID] = rng
			case "incompatible":
				info.Breaks[dep.ModID] = rng
			case "discouraged":
				info.Conflicts[dep.ModID] = rng
			}
		}
	}
	addJarJar(zr, info, files)
	return info, nil
}

// dependencyType reads NeoForge's type, falling back to Forge's mandatory flag.
func dependencyType(typ string, mandatory any) string {
	if typ != "" {
		return strings.ToLower(typ)
	}
	if b, ok := mandatory.(bool); ok && !b {
		return "optional"
	}
	return "required"
}

func addJarJar(zr *zip.Reader, info *Info, files []string) {
	f := lookup(zr, "META-INF/jarjar/metadata.json")
	if f == nil {
		return
	}
	data, err := readFile(f)
	if err != nil {
		return
	}
	var meta struct {
		Jars []struct {
			Path string `json:"path"`
		} `json:"jars"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return
	}
	for _, jar := range meta.Jars {
		addNested(zr, info, jar.Path, files)
	}
}

func manifestAttribute(zr *zip.Reader, name string) string {
	f := lookup(zr, "META-INF/MANIFEST.MF")
	if f == nil {
		return ""
	}
	data, err := readFile(f)
	if err != nil {
		return ""
	}
	var key, value string
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, " ") {
			value += line[1:]
			continue
		}
		if strings.EqualFold(key, name) {
			return strings.TrimSpace(value)
		}
		if line == "" {
			return ""
		}
		key, value, _ = strings.Cut(line, ":")
	}
	if strings.EqualFold(key, name) {
		return strings.TrimSpace(value)
	}
	return ""
}
