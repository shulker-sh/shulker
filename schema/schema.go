package schema

import (
	"bytes"
	"embed"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed manifest-v1.json lock-v1.json
var files embed.FS

type Kind string

const (
	Manifest Kind = "manifest-v1.json"
	Lock     Kind = "lock-v1.json"
)

func Compile(kind Kind) (*jsonschema.Schema, error) {
	raw, err := files.ReadFile(string(kind))
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", kind, err)
	}
	id, _ := doc.(map[string]any)["$id"].(string)
	if id == "" {
		return nil, fmt.Errorf("%s has no $id", kind)
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(id, doc); err != nil {
		return nil, err
	}
	return c.Compile(id)
}

func Validate(kind Kind, data []byte) error {
	s, err := Compile(kind)
	if err != nil {
		return err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return s.Validate(doc)
}
