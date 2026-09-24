// Package managed reads and replaces the files shulker writes and reads back, one call per file,
// with the kind's schema and error code taken from schema.ManagedFiles.
package managed

import (
	"bytes"
	"encoding/json"
	"os"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

// Read reads the managed file of kind at path into v. A file that can't be opened is the os error
// as it came, so a caller tells a missing file apart. One this shulker can't read is schema-newer
// when a newer shulker wrote it and the kind's own code otherwise: the marker is checked first, then
// the schema for a kind that validates, then the JSON itself; the error's Cause holds the reason
// without the path.
func Read(kind schema.Kind, path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return Decode(kind, path, data, v)
}

// Decode is Read for data already in memory; file names it in errors.
func Decode(kind schema.Kind, file string, data []byte, v any) error {
	m := managed(kind)
	if m.EmptyIsZero && len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if !m.MarkerOptional || hasMarker(data) {
		if err := schema.CheckMarker(kind, m.Code, file, data); err != nil {
			return err
		}
	}
	if m.Validates {
		if err := schema.Validate(kind, data); err != nil {
			return schema.Invalid(m.Code, file, data, err)
		}
	}
	if err := schema.DecodeInto(data, v); err != nil {
		return schema.Invalid(m.Code, file, data, err)
	}
	return nil
}

// Replace writes v as the file of kind at path over one this shulker couldn't read, keeping the
// old file as <name>.replaced; kept is where it went, empty when there was none. It refuses,
// touching nothing, a v this shulker couldn't read back: one without the kind's own $schema line,
// or one that fails the schema of a kind that validates.
func Replace(kind schema.Kind, path string, v any) (kept string, err error) {
	data, err := fsutil.MarshalJSON(v)
	if err != nil {
		return "", err
	}
	if err := Decode(kind, path, data, new(any)); err != nil {
		e := out.AsError(err)
		e.Message = "refusing to write " + e.Message
		return "", e
	}
	return fsutil.Replace(path, data)
}

func managed(kind schema.Kind) schema.Managed {
	m, ok := schema.ManagedFiles[kind]
	if !ok {
		panic("managed: " + string(kind) + " is not a managed file")
	}
	return m
}

func hasMarker(data []byte) bool {
	var head struct {
		Schema json.RawMessage `json:"$schema"`
	}
	return json.Unmarshal(data, &head) != nil || head.Schema != nil
}
