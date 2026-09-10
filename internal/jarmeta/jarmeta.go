package jarmeta

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrNoMetadata = errors.New("no mod metadata found in jar")

type Info struct {
	ID      string
	Version string
	Loader  string
	Side    string
	Depends map[string]string
}

func Read(path string) (*Info, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		switch f.Name {
		case "fabric.mod.json":
			return readFabric(f)
		}
	}
	return nil, fmt.Errorf("%s: %w", path, ErrNoMetadata)
}

func readFabric(f *zip.File) (*Info, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	var raw struct {
		ID          string          `json:"id"`
		Version     string          `json:"version"`
		Environment string          `json:"environment"`
		Depends     json.RawMessage `json:"depends"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("fabric.mod.json: %w", err)
	}
	if raw.ID == "" {
		return nil, errors.New("fabric.mod.json: missing id")
	}
	info := &Info{ID: raw.ID, Version: raw.Version, Loader: "fabric", Side: fabricSide(raw.Environment), Depends: map[string]string{}}
	if len(raw.Depends) > 0 {
		var depends map[string]any
		if err := json.Unmarshal(raw.Depends, &depends); err == nil {
			for id, rng := range depends {
				info.Depends[id] = rangeString(rng)
			}
		}
	}
	return info, nil
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
