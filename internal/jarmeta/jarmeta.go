package jarmeta

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrNoMetadata = errors.New("no mod metadata found in jar")

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
	Provides map[string]string
}

func Read(path string) (*Info, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer zr.Close()
	info, err := readZip(&zr.Reader)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return info, nil
}

func readZip(zr *zip.Reader) (*Info, error) {
	if f := lookup(zr, "quilt.mod.json"); f != nil {
		return readQuilt(zr, f)
	}
	if f := lookup(zr, "fabric.mod.json"); f != nil {
		return readFabric(zr, f)
	}
	return nil, ErrNoMetadata
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

func readFabric(zr *zip.Reader, f *zip.File) (*Info, error) {
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
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("fabric.mod.json: %w", err)
	}
	if raw.ID == "" {
		return nil, errors.New("fabric.mod.json: missing id")
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
		addNested(zr, info, nested.File)
	}
	return info, nil
}

func readQuilt(zr *zip.Reader, f *zip.File) (*Info, error) {
	data, err := readFile(f)
	if err != nil {
		return nil, err
	}
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
		return nil, fmt.Errorf("quilt.mod.json: %w", err)
	}
	if raw.Loader.ID == "" {
		return nil, errors.New("quilt.mod.json: missing quilt_loader.id")
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
		addNested(zr, info, file)
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

func addNested(zr *zip.Reader, info *Info, name string) {
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
	child, err := readZip(inner)
	if err != nil {
		return
	}
	info.Provides[child.ID] = child.Version
	for id, v := range child.Provides {
		info.Provides[id] = v
	}
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
