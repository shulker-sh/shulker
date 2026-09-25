package jarmeta

import (
	"archive/zip"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
)

type mcmodEntry struct {
	ModID                    string   `json:"modid"`
	Version                  string   `json:"version"`
	UseDependencyInformation bool     `json:"useDependencyInformation"`
	RequiredMods             []string `json:"requiredMods"`
	Dependencies             []string `json:"dependencies"`
}

type legacyMod struct {
	id, version, dependencies string
	side                      string
}

// legacyBuiltins are the ids FML itself provides besides forge and minecraft.
var legacyBuiltins = map[string]bool{"FML": true, "mcp": true}

// readLegacyForge reads a jar the way FML before 1.13 finds mods in it: each class's @Mod
// annotation, a coremod's DummyModContainer and a package's @API, with mcmod.info filling in a
// missing version and, when it opts in, the dependencies. A jar declaring no mod, such as a coremod
// that only transforms classes, yields an Info with no ID. Like FML, it ignores an mcmod.info it
// can't parse even leniently.
func readLegacyForge(zr *zip.Reader) *Info {
	entries, _ := readMcmodInfo(zr)
	byID := map[string]mcmodEntry{}
	for _, e := range entries {
		if _, seen := byID[e.ModID]; !seen {
			byID[e.ModID] = e
		}
	}
	mods, apis := declaredMods(zr)
	if len(mods) == 0 && len(entries) > 0 {
		mods = []legacyMod{{id: entries[0].ModID}}
	}
	info := &Info{
		Loader:          "forge",
		Side:            "both",
		Depends:         map[string]Range{},
		Breaks:          map[string]Range{},
		Conflicts:       map[string]Range{},
		Recommends:      map[string]Range{},
		Suggests:        map[string]Range{},
		Optional:        map[string]Range{},
		Provides:        map[string]string{},
		UsesMavenRanges: true,
	}
	own := map[string]bool{}
	for _, m := range mods {
		own[m.id] = true
	}
	for id, version := range apis {
		if !own[id] {
			info.Provides[id] = version
		}
	}
	if len(mods) == 0 {
		return info
	}
	for id := range apis {
		own[id] = true
	}
	if len(entries) > 0 {
		for i, m := range mods {
			if m.id == entries[0].ModID {
				mods[0], mods[i] = mods[i], mods[0]
				break
			}
		}
	}
	for i := range mods {
		if mods[i].version == "" {
			mods[i].version = byID[mods[i].id].Version
		}
	}
	info.ID, info.Version = mods[0].id, mods[0].version
	if mods[0].side != "" {
		info.Side = mods[0].side
	}
	for _, m := range mods[1:] {
		info.Provides[m.id] = m.version
	}
	for _, m := range mods {
		if e, ok := byID[m.id]; ok && e.UseDependencyInformation {
			addReferences(info.Depends, e.RequiredMods, own)
			addReferences(info.Optional, e.Dependencies, own)
			continue
		}
		addDependencyString(info, m.dependencies, own)
	}
	return info
}

func readMcmodInfo(zr *zip.Reader) ([]mcmodEntry, error) {
	f := lookup(zr, "mcmod.info")
	if f == nil {
		return nil, nil
	}
	data, err := readFile(f)
	if err != nil {
		return nil, err
	}
	data = lenientJSON(gsonStrings(data))
	var list []mcmodEntry
	if err := json.Unmarshal(data, &list); err == nil {
		return list, nil
	}
	var wrapped struct {
		ModList []mcmodEntry `json:"modList"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.ModList, nil
}

// lenientJSON quotes the bare words lenient Gson reads as strings, like [mod_minecraftForge], so
// encoding/json accepts them. It expects gsonStrings to have run first.
func lenientJSON(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString, escaped := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && isBareWordByte(c):
			j := i
			for j < len(data) && isBareWordByte(data[j]) {
				j++
			}
			word := string(data[i:j])
			if isJSONLiteral(word) {
				out = append(out, word...)
			} else {
				out = append(out, '"')
				out = append(out, word...)
				out = append(out, '"')
			}
			i = j - 1
			continue
		}
		out = append(out, c)
	}
	return out
}

func isBareWordByte(c byte) bool {
	return !strings.ContainsRune("{}[],:\" \t\r\n", rune(c))
}

func isJSONLiteral(word string) bool {
	switch word {
	case "true", "false", "null":
		return true
	}
	var n json.Number
	return json.Unmarshal([]byte(word), &n) == nil
}

// declaredMods are the mods a jar's classes declare to FML, @Mod classes first and then the ids its
// DummyModContainers take, and the ids its @API packages provide.
func declaredMods(zr *zip.Reader) ([]legacyMod, map[string]string) {
	var mods, containers []legacyMod
	apis := map[string]string{}
	for _, d := range readClasses(zr) {
		if d.mod != nil {
			m := legacyMod{}
			m.id, _ = d.mod["modid"].(string)
			m.version, _ = d.mod["version"].(string)
			m.dependencies, _ = d.mod["dependencies"].(string)
			if b, _ := d.mod["clientSideOnly"].(bool); b {
				m.side = "client"
			} else if b, _ := d.mod["serverSideOnly"].(bool); b {
				m.side = "server"
			}
			if m.id != "" {
				mods = append(mods, m)
			}
		}
		if id := d.container["modId"]; id != "" {
			containers = append(containers, legacyMod{id: id, version: d.container["version"]})
		}
		if id, _ := d.api["provides"].(string); id != "" {
			apis[id], _ = d.api["apiVersion"].(string)
		}
	}
	return append(mods, containers...), apis
}

// readClasses reads what each class in the jar declares to FML, in the jar's order. Inflating
// every class dominates reading a legacy jar, so the classes are read in parallel.
func readClasses(zr *zip.Reader) []classDeclarations {
	var classes []*zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".class") {
			classes = append(classes, f)
		}
	}
	found := make([]classDeclarations, len(classes))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(classes)) {
		wg.Go(func() {
			for i := range next {
				data, err := readFile(classes[i])
				if err != nil {
					continue
				}
				if d, err := readClass(data); err == nil {
					found[i] = d
				}
			}
		})
	}
	for i := range classes {
		next <- i
	}
	close(next)
	wg.Wait()
	return found
}

// addDependencyString reads @Mod's dependencies the way FML's DependencyParser does:
// "required-after:forge@[14.23,);after:jei". Required ones on both sides are dependencies; one
// required on a single side, or only ordered after, must match its range when present.
func addDependencyString(info *Info, deps string, own map[string]bool) {
	for _, dep := range strings.Split(deps, ";") {
		instructions, target, ok := strings.Cut(strings.TrimSpace(dep), ":")
		if !ok || strings.HasPrefix(target, "*") {
			continue
		}
		required, sided := false, false
		for _, part := range strings.Split(instructions, "-") {
			switch strings.TrimSpace(part) {
			case "required":
				required = true
			case "client", "server":
				sided = true
			}
		}
		id, rng := versionReference(target)
		if id == "" || own[id] || legacyBuiltins[id] {
			continue
		}
		if required && !sided {
			info.Depends[id] = Range{rng}
		} else {
			info.Optional[id] = Range{rng}
		}
	}
}

func addReferences(into map[string]Range, refs []string, own map[string]bool) {
	for _, ref := range refs {
		if id, rng := versionReference(ref); id != "" && !own[id] && !legacyBuiltins[id] {
			into[id] = Range{rng}
		}
	}
}

// versionReference splits FML's "modid@range", where a bare id takes any version. Forge before 1.8
// named itself Forge, which the loader's own id stands for.
func versionReference(ref string) (string, string) {
	id, rng, _ := strings.Cut(strings.TrimSpace(ref), "@")
	if rng = strings.TrimSpace(rng); rng == "" {
		rng = "*"
	}
	if id = strings.TrimSpace(id); id == "Forge" {
		id = "forge"
	}
	return id, rng
}
