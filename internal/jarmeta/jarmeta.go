// Package jarmeta reads the mod metadata inside a jar the way each loader reads it.
package jarmeta

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Info is the mod a jar declares. Each dependency map is keyed by mod id and holds a version range.
type Info struct {
	ID         string
	Version    string
	Loader     string
	Side       string
	Depends    map[string]string
	Breaks     map[string]string
	Conflicts  map[string]string
	Recommends map[string]string
	Suggests   map[string]string
	// Optional dependencies aren't required, but a present mod must match the range.
	Optional map[string]string
	// Provides are the ids the jar itself declares; a nested jar's are on its own Info in Nested.
	Provides map[string]string
	Nested   []*Info
	// UsesMavenRanges marks ranges written in Maven syntax (NeoForge and Forge) rather than Fabric's.
	UsesMavenRanges bool
}

var allFiles = []string{"quilt.mod.json", "fabric.mod.json", "META-INF/neoforge.mods.toml", "META-INF/mods.toml"}

// Read reads the metadata the named loader would, so a jar built for several loaders yields the
// right one. An unknown loader reads whichever metadata the jar has. Errors call the jar name, since
// path may be a cache object named by its hash.
func Read(path, name, loaderName string) (*Info, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, metadataInvalid(name, "zip", err)
	}
	defer zr.Close()
	files := allFiles
	if l, ok := loader.Lookup(loaderName); ok {
		files = l.MetadataFiles
	}
	info, err := readZip(&zr.Reader, files)
	if errors.Is(err, errNoMetadata) {
		if loaderName == "" {
			return nil, out.Errorf("jar-metadata-missing", "%s holds no mod metadata", name)
		}
		return nil, out.Errorf("jar-metadata-missing", "%s holds no mod metadata for %s", name, loaderName)
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
		ID          string          `json:"id"`
		Version     string          `json:"version"`
		Environment string          `json:"environment"`
		Depends     json.RawMessage `json:"depends"`
		Breaks      json.RawMessage `json:"breaks"`
		Conflicts   json.RawMessage `json:"conflicts"`
		Recommends  json.RawMessage `json:"recommends"`
		Suggests    json.RawMessage `json:"suggests"`
		Provides    []string        `json:"provides"`
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
		Optional:   map[string]string{},
		Provides:   map[string]string{},
	}
	for _, id := range raw.Provides {
		info.Provides[id] = raw.Version
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
			ID       string            `json:"id"`
			Version  string            `json:"version"`
			Depends  []json.RawMessage `json:"depends"`
			Breaks   []json.RawMessage `json:"breaks"`
			Provides []json.RawMessage `json:"provides"`
			Jars     []string          `json:"jars"`
		} `json:"quilt_loader"`
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
		Depends:    map[string]string{},
		Breaks:     map[string]string{},
		Conflicts:  map[string]string{},
		Recommends: map[string]string{},
		Suggests:   map[string]string{},
		Optional:   map[string]string{},
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
	for _, file := range raw.Loader.Jars {
		addNested(zr, info, file, files)
	}
	return info, nil
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

func quiltRange(v any) string {
	switch r := v.(type) {
	case string:
		return r
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
				if strings.Contains(part, "||") {
					return "*"
				}
				parts = append(parts, part)
			}
			return strings.Join(parts, " ")
		}
	}
	return "*"
}

func anyOf(list []any) string {
	parts := make([]string, 0, len(list))
	for _, item := range list {
		parts = append(parts, quiltRange(item))
	}
	return strings.Join(parts, " || ")
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

func rangeMap(raw json.RawMessage) map[string]string {
	m := map[string]string{}
	if len(raw) == 0 {
		return m
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return m
	}
	for id, rng := range decoded {
		m[id] = rangeString(rng)
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

func rangeString(v any) string {
	switch r := v.(type) {
	case string:
		return r
	case []any:
		out := ""
		for i, item := range r {
			if s, ok := item.(string); ok {
				if i > 0 {
					out += " || "
				}
				out += s
			}
		}
		return out
	}
	return "*"
}
