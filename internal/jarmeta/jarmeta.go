package jarmeta

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	Provides   map[string]string
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
	for _, f := range zr.File {
		switch f.Name {
		case "fabric.mod.json":
			return readFabric(zr, f)
		}
	}
	return nil, ErrNoMetadata
}

func readFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
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
		Provides:   map[string]string{},
	}
	for _, id := range raw.Provides {
		info.Provides[id] = raw.Version
	}
	for _, nested := range raw.Jars {
		nf := lookup(zr, nested.File)
		if nf == nil {
			continue
		}
		blob, err := readFile(nf)
		if err != nil {
			continue
		}
		inner, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
		if err != nil {
			continue
		}
		child, err := readZip(inner)
		if err != nil {
			continue
		}
		info.Provides[child.ID] = child.Version
		for id, v := range child.Provides {
			info.Provides[id] = v
		}
	}
	return info, nil
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
