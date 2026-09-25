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
	Dependencies map[string]toml.Primitive `toml:"dependencies"`
}

type modsTOMLDependency struct {
	ModID        string `toml:"modId"`
	Type         string `toml:"type"`
	Mandatory    any    `toml:"mandatory"`
	VersionRange string `toml:"versionRange"`
}

// readModsTOML reads NeoForge's neoforge.mods.toml and Forge's mods.toml. Both leave the side to
// the provider, and FML reads dependencies only from tables keyed by one of the jar's own mods.
func readModsTOML(zr *zip.Reader, f *zip.File, files []string) (*Info, error) {
	data, err := readFile(f)
	if err != nil {
		return nil, err
	}
	var raw modsTOML
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
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
	info := newFMLInfo(raw.Mods[0].ModID, version(raw.Mods[0].Version), modsTOMLLoader(f.Name))
	own := map[string]bool{}
	for _, m := range raw.Mods {
		own[m.ModID] = true
		if m.ModID != info.ID {
			info.Provides[m.ModID] = version(m.Version)
		}
	}
	for _, m := range raw.Mods {
		table, ok := raw.Dependencies[m.ModID]
		if !ok {
			continue
		}
		var deps []modsTOMLDependency
		if err := md.PrimitiveDecode(table, &deps); err != nil {
			return nil, err
		}
		for _, dep := range deps {
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

// readFMLLibrary reads a jar with no mods.toml that FML still loads, as a library, language
// provider or game library, because its manifest names an FMLModType. It declares no mod id, and
// FML versions it by its Implementation-Version; its jar-in-jar mods load as any mod's do.
func readFMLLibrary(zr *zip.Reader, files []string) *Info {
	loaderName := ""
	for _, name := range files {
		if loaderName = modsTOMLLoader(name); loaderName != "" {
			break
		}
	}
	if loaderName == "" || manifestAttribute(zr, "FMLModType") == "" {
		return nil
	}
	version := manifestAttribute(zr, "Implementation-Version")
	if version == "" {
		version = "1"
	}
	info := newFMLInfo("", version, loaderName)
	addJarJar(zr, info, files)
	return info
}

func newFMLInfo(id, version, loaderName string) *Info {
	return &Info{
		ID:              id,
		Version:         version,
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
}

func modsTOMLLoader(file string) string {
	switch file {
	case "META-INF/neoforge.mods.toml":
		return "neoforge"
	case "META-INF/mods.toml":
		return "forge"
	}
	return ""
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
