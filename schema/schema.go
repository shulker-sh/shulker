// Package schema embeds the JSON schemas shulker's files carry as $schema, validates a file against
// its kind and phrases what is wrong with one. It lives at the module root so schema/v1 is the
// public path the site serves.
package schema

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"shulker.sh/shulker/internal/fsutil"
)

//go:embed v1/manifest.json v1/lock.json v1/instance.json v1/registry.json v1/accounts.json v1/state.json v1/local.json v1/config.json v1/overrides.json
var files embed.FS

type Kind string

const (
	Manifest Kind = "v1/manifest.json"
	Lock     Kind = "v1/lock.json"
	Instance Kind = "v1/instance.json"
	Registry Kind = "v1/registry.json"
	Accounts Kind = "v1/accounts.json"
	State    Kind = "v1/state.json"
	Local    Kind = "v1/local.json"
	Config   Kind = "v1/config.json"
	// Overrides is the override folders sidecar an export writes at an archive's root.
	Overrides Kind = "v1/overrides.json"
)

type compiled struct {
	schema *jsonschema.Schema
	err    error
}

var compiledSchemas sync.Map

// Compile returns the compiled schema, compiling each kind once.
func Compile(kind Kind) (*jsonschema.Schema, error) {
	if c, ok := compiledSchemas.Load(kind); ok {
		return c.(compiled).schema, c.(compiled).err
	}
	s, err := compile(kind)
	compiledSchemas.Store(kind, compiled{s, err})
	return s, err
}

func compile(kind Kind) (*jsonschema.Schema, error) {
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
	c.RegisterFormat(relativePathFormat)
	if err := c.AddResource(id, doc); err != nil {
		return nil, err
	}
	return c.Compile(id)
}

// relativePathFormat holds a relativePath to fsutil.IsPortableLocal, which also refuses the Windows
// device names the pattern leaves out.
var relativePathFormat = &jsonschema.Format{Name: "relative-path", Validate: func(v any) error {
	if rel, ok := v.(string); ok && !fsutil.IsPortableLocal(rel) {
		return errors.New("leaves its folder or names a Windows device like CON or NUL")
	}
	return nil
}}

func Raw(kind Kind) ([]byte, error) {
	return files.ReadFile(string(kind))
}

func Validate(kind Kind, data []byte) error {
	s, err := Compile(kind)
	if err != nil {
		return err
	}
	doc, err := Decode(data)
	if err != nil {
		return err
	}
	return s.Validate(doc)
}
