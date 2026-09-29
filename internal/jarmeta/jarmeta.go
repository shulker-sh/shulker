// Package jarmeta reads the mod metadata inside a jar the way each loader reads it.
package jarmeta

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
)

var errNoMetadata = errors.New("no mod metadata found in jar")

// fileError is a metadata file in the jar that doesn't read, named so the error can say which.
type fileError struct {
	name string
	err  error
}

func (e *fileError) Error() string { return e.name + ": " + e.err.Error() }

// Range is a declared version range. A version that meets any one alternative meets the range;
// only an array in the metadata makes more than one, never "||" inside a single string.
type Range []string

func (r Range) String() string { return strings.Join(r, " || ") }

// Info is the mod a jar declares. Each dependency map is keyed by mod id and holds a version range.
type Info struct {
	ID      string
	Version string
	Loader  string
	Side    string
	// SideFromDependencies marks a Side read from a mods.toml's dependency sides, since the file
	// names none of its own.
	SideFromDependencies bool
	Depends              map[string]Range
	Breaks               map[string]Range
	Conflicts            map[string]Range
	Recommends           map[string]Range
	Suggests             map[string]Range
	// Optional dependencies aren't required, but a present mod must match the range.
	Optional map[string]Range
	// Provides are the ids the jar itself declares; a nested jar's are on its own Info in Nested.
	Provides map[string]string
	// DependencySides are the dependencies FML checks on one side only, "client" or "server" by id.
	DependencySides map[string]string
	Nested          []*Info
	// Entrypoints are the classes the loader calls into, each under the kind of entrypoint it is.
	Entrypoints []Entrypoint
	// Mixins are the mixin config files the jar asks the loader to apply.
	Mixins []string
	// UsesMavenRanges marks ranges written in Maven syntax (NeoForge and Forge) rather than Fabric's.
	UsesMavenRanges bool
}

// Entrypoint is a class or member a jar asks its loader to call, such as Fabric's "main" or
// "client" entrypoints.
type Entrypoint struct {
	Kind  string
	Value string
}

var allFiles = []string{"quilt.mod.json", "fabric.mod.json", "META-INF/neoforge.mods.toml", "META-INF/mods.toml"}

// Read reads the metadata loader l would, so a jar built for several loaders yields the right one.
// The zero Loader reads whichever metadata the jar has. Errors call the jar name, since path may be
// a cache object named by its hash.
func Read(path, name string, l loader.Loader) (*Info, error) {
	if l.ModAnnotations {
		return readLegacyJar(path, name)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, metadataInvalid(name, "zip", err)
	}
	defer zr.Close()
	files := allFiles
	if l.Name != "" {
		files = l.MetadataFiles
	}
	info, err := readZip(&zr.Reader, files)
	if errors.Is(err, errNoMetadata) {
		if l.Name == "" {
			return nil, out.Errorf("jar-metadata-missing", "%s holds no mod metadata", name)
		}
		return nil, out.Errorf("jar-metadata-missing", "%s holds no mod metadata for %s", name, l.Name)
	}
	var bad *fileError
	if errors.As(err, &bad) {
		return nil, metadataInvalid(name, bad.name, bad.err)
	}
	if err != nil {
		return nil, metadataInvalid(name, "zip", err)
	}
	return info, nil
}

// readLegacyJar reads a jar for a loader that finds mods by their annotations. That inflates every
// class, and the zip reader's many small reads cost more than holding the whole jar in memory.
func readLegacyJar(path, name string) (*Info, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, metadataInvalid(name, "zip", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, metadataInvalid(name, "zip", err)
	}
	return readLegacyForge(zr), nil
}

// ReadZip reads the metadata loader l would from an open jar, returning nil for a jar that declares
// none.
func ReadZip(zr *zip.Reader, l loader.Loader) (*Info, error) {
	if l.ModAnnotations {
		return readLegacyForge(zr), nil
	}
	files := allFiles
	if l.Name != "" {
		files = l.MetadataFiles
	}
	info, err := readZip(zr, files)
	if errors.Is(err, errNoMetadata) {
		return nil, nil
	}
	return info, err
}

func metadataInvalid(name, source string, err error) *out.Error {
	return out.Errorf("jar-metadata-invalid", "shulker can't read the mod metadata in %s", name).WithCause(source, err)
}

func readZip(zr *zip.Reader, files []string) (*Info, error) {
	for _, name := range files {
		f := lookup(zr, name)
		if f == nil {
			continue
		}
		var info *Info
		var err error
		switch name {
		case "quilt.mod.json":
			info, err = readQuilt(zr, f, files)
		case "fabric.mod.json":
			info, err = readFabric(zr, f, files)
		default:
			info, err = readModsTOML(zr, f, files)
		}
		if err != nil {
			return nil, &fileError{name: name, err: err}
		}
		return info, nil
	}
	if info := readFMLLibrary(zr, files); info != nil {
		return info, nil
	}
	return nil, errNoMetadata
}

func readFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), nil
}

// gsonStrings rewrites what Fabric's and Quilt's Gson-derived readers accept inside a string, and
// encoding/json doesn't, into standard JSON: raw control characters, an escaped apostrophe, and a
// backslash before a raw newline. Published jars rely on all three.
func gsonStrings(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString, escaped := false, false
	for _, c := range data {
		switch {
		case escaped:
			escaped = false
			if c == '\'' {
				out[len(out)-1] = c
				continue
			}
			if c == '\n' {
				c = 'n'
			}
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case inString && c < 0x20:
			out = fmt.Appendf(out, `\u%04x`, c)
			continue
		}
		out = append(out, c)
	}
	return out
}

func readFabric(zr *zip.Reader, f *zip.File, files []string) (*Info, error) {
	data, err := readFile(f)
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID          string                     `json:"id"`
		Version     string                     `json:"version"`
		Environment string                     `json:"environment"`
		Depends     json.RawMessage            `json:"depends"`
		Breaks      json.RawMessage            `json:"breaks"`
		Conflicts   json.RawMessage            `json:"conflicts"`
		Recommends  json.RawMessage            `json:"recommends"`
		Suggests    json.RawMessage            `json:"suggests"`
		Provides    []string                   `json:"provides"`
		Entrypoints map[string]json.RawMessage `json:"entrypoints"`
		Mixins      []json.RawMessage          `json:"mixins"`
		Jars        []struct {
			File string `json:"file"`
		} `json:"jars"`
	}
	// Fabric stops reading at the end of the root object, so anything after it is ignored.
	if err := json.NewDecoder(bytes.NewReader(gsonStrings(data))).Decode(&raw); err != nil {
		return nil, err
	}
	if raw.ID == "" {
		return nil, errors.New("missing id")
	}
	info := &Info{
		ID:         raw.ID,
		Version:    raw.Version,
		Loader:     "fabric",
		Side:       fabricSide(raw.Environment),
		Depends:    rangeMap(raw.Depends),
		Breaks:     rangeMap(raw.Breaks),
		Conflicts:  rangeMap(raw.Conflicts),
		Recommends: rangeMap(raw.Recommends),
		Suggests:   rangeMap(raw.Suggests),
		Optional:   map[string]Range{},
		Provides:   map[string]string{},
	}
	for _, id := range raw.Provides {
		info.Provides[id] = raw.Version
	}
	info.Entrypoints = entrypoints(raw.Entrypoints)
	for _, m := range raw.Mixins {
		info.Mixins = append(info.Mixins, stringOr(m, "config")...)
	}
	for _, nested := range raw.Jars {
		addNested(zr, info, nested.File, files)
	}
	return info, nil
}

func readQuilt(zr *zip.Reader, f *zip.File, files []string) (*Info, error) {
	data, err := readFile(f)
	if err != nil {
		return nil, err
	}
	data = gsonStrings(data)
	var raw struct {
		Loader struct {
			ID          string                     `json:"id"`
			Version     string                     `json:"version"`
			Depends     []json.RawMessage          `json:"depends"`
			Breaks      []json.RawMessage          `json:"breaks"`
			Provides    []json.RawMessage          `json:"provides"`
			Entrypoints map[string]json.RawMessage `json:"entrypoints"`
			Jars        []string                   `json:"jars"`
		} `json:"quilt_loader"`
		Mixin     json.RawMessage `json:"mixin"`
		Minecraft struct {
			Environment string `json:"environment"`
		} `json:"minecraft"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw.Loader.ID == "" {
		return nil, errors.New("missing quilt_loader.id")
	}
	info := &Info{
		ID:         raw.Loader.ID,
		Version:    raw.Loader.Version,
		Loader:     "quilt",
		Side:       quiltSide(raw.Minecraft.Environment),
		Depends:    map[string]Range{},
		Breaks:     map[string]Range{},
		Conflicts:  map[string]Range{},
		Recommends: map[string]Range{},
		Suggests:   map[string]Range{},
		Optional:   map[string]Range{},
		Provides:   map[string]string{},
	}
	for _, entry := range raw.Loader.Depends {
		if dep, ok := quiltDependency(entry); ok {
			if dep.Optional {
				info.Optional[dep.id()] = quiltRange(dep.Versions)
			} else {
				info.Depends[dep.id()] = quiltRange(dep.Versions)
			}
		}
	}
	for _, entry := range raw.Loader.Breaks {
		if dep, ok := quiltDependency(entry); ok {
			info.Breaks[dep.id()] = quiltRange(dep.Versions)
		}
	}
	for _, entry := range raw.Loader.Provides {
		var p struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		}
		if json.Unmarshal(entry, &p.ID) != nil && json.Unmarshal(entry, &p) != nil {
			continue
		}
		if p.Version == "" {
			p.Version = raw.Loader.Version
		}
		info.Provides[stripGroup(p.ID)] = p.Version
	}
	info.Entrypoints = entrypoints(raw.Loader.Entrypoints)
	info.Mixins = stringOr(raw.Mixin, "config")
	for _, file := range raw.Loader.Jars {
		addNested(zr, info, file, files)
	}
	return info, nil
}

// entrypoints reads Fabric's and Quilt's entrypoints, where each kind holds one entry or an array
// of them, and an entry is a string or an object with its "value" and an "adapter".
func entrypoints(raw map[string]json.RawMessage) []Entrypoint {
	var found []Entrypoint
	for _, kind := range slices.Sorted(maps.Keys(raw)) {
		for _, value := range stringOr(raw[kind], "value") {
			found = append(found, Entrypoint{Kind: kind, Value: value})
		}
	}
	return found
}

// stringOr reads a string, an object holding the string under field, or an array of either.
func stringOr(raw json.RawMessage, field string) []string {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		list = []json.RawMessage{raw}
	}
	var found []string
	for _, item := range list {
		var s string
		if json.Unmarshal(item, &s) != nil {
			var obj map[string]any
			if json.Unmarshal(item, &obj) != nil {
				continue
			}
			s, _ = obj[field].(string)
		}
		if s != "" {
			found = append(found, s)
		}
	}
	return found
}

type quiltDep struct {
	ID       string          `json:"id"`
	Versions any             `json:"versions"`
	Optional bool            `json:"optional"`
	Unless   json.RawMessage `json:"unless"`
}

func (d quiltDep) id() string { return stripGroup(d.ID) }

// quiltDependency skips any-of arrays and `unless` entries: neither fits a flat id → range map, and
// guessing would report problems the loader doesn't have.
func quiltDependency(entry json.RawMessage) (quiltDep, bool) {
	var dep quiltDep
	if json.Unmarshal(entry, &dep.ID) == nil {
		return dep, dep.ID != ""
	}
	if json.Unmarshal(entry, &dep) != nil || dep.ID == "" || len(dep.Unless) > 0 {
		return quiltDep{}, false
	}
	return dep, true
}

func stripGroup(id string) string {
	if i := strings.LastIndexByte(id, ':'); i >= 0 {
		return id[i+1:]
	}
	return id
}

func quiltRange(v any) Range {
	switch r := v.(type) {
	case string:
		return Range{r}
	case []any:
		return anyOf(r)
	case map[string]any:
		if list, ok := r["any"].([]any); ok {
			return anyOf(list)
		}
		if list, ok := r["all"].([]any); ok {
			parts := make([]string, 0, len(list))
			for _, item := range list {
				part := quiltRange(item)
				if len(part) != 1 {
					return Range{"*"}
				}
				parts = append(parts, part[0])
			}
			return Range{strings.Join(parts, " ")}
		}
	}
	return Range{"*"}
}

func anyOf(list []any) Range {
	var alts Range
	for _, item := range list {
		alts = append(alts, quiltRange(item)...)
	}
	if len(alts) == 0 {
		return Range{""}
	}
	return alts
}

func quiltSide(env string) string {
	switch env {
	case "client":
		return "client"
	case "dedicated_server":
		return "server"
	}
	return "both"
}

func addNested(zr *zip.Reader, info *Info, name string, files []string) {
	nf := lookup(zr, name)
	if nf == nil {
		return
	}
	blob, err := readFile(nf)
	if err != nil {
		return
	}
	inner, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return
	}
	child, err := readZip(inner, files)
	if err != nil {
		return
	}
	if child.ID == "" {
		info.Nested = append(info.Nested, child.Nested...)
		return
	}
	info.Nested = append(info.Nested, child)
}

// AllProvides is every id the jar makes available besides its own: what it provides, and each
// nested jar's id and provides, at any depth.
func (i *Info) AllProvides() map[string]string {
	all := map[string]string{}
	for id, v := range i.Provides {
		all[id] = v
	}
	for _, n := range i.Nested {
		all[n.ID] = n.Version
		for id, v := range n.AllProvides() {
			all[id] = v
		}
	}
	return all
}

func lookup(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func rangeMap(raw json.RawMessage) map[string]Range {
	m := map[string]Range{}
	if len(raw) == 0 {
		return m
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return m
	}
	for id, rng := range decoded {
		m[id] = fabricRange(rng)
	}
	return m
}

func fabricSide(env string) string {
	switch env {
	case "client":
		return "client"
	case "server":
		return "server"
	}
	return "both"
}

func fabricRange(v any) Range {
	switch r := v.(type) {
	case string:
		return Range{r}
	case []any:
		var alts Range
		for _, item := range r {
			if s, ok := item.(string); ok {
				alts = append(alts, s)
			}
		}
		return alts
	}
	return Range{"*"}
}
