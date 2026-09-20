package schema

import (
	"encoding/json"
	"errors"
	"path"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/out"
)

// Base is the prefix every published schema URL carries; the vN segment after it is the version of
// the file that names it.
const Base = "https://shulker.sh/schema/"

// URL is the $schema line a managed file of this kind is written with.
func URL(kind Kind) string { return Base + string(kind) }

// markerVersion reads the version out of a $schema URL naming file. A URL of another file, another
// host, or with no vN segment has no version, so two markers compare only when they name the same
// file.
func markerVersion(url, file string) (int, bool) {
	rest, ok := strings.CutPrefix(url, Base)
	if !ok {
		return 0, false
	}
	segment, name, ok := strings.Cut(rest, "/")
	if !ok || name != file {
		return 0, false
	}
	digits, ok := strings.CutPrefix(segment, "v")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// CheckMarker reads a managed file's $schema line and reports what shulker can do with the file.
// Its own version passes; a higher one is schema-newer, which `shulker self update` fixes; a lower,
// foreign or absent marker is a file this shulker can't read, under the file's own code.
func CheckMarker(kind Kind, code, file string, data []byte) error {
	want, ok := markerVersion(URL(kind), path.Base(string(kind)))
	if !ok {
		return errors.New("schema kind " + string(kind) + " has no version")
	}
	var head struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return Invalid(code, file, data, err)
	}
	if got, ok := markerVersion(head.Schema, path.Base(string(kind))); ok {
		switch {
		case got == want:
			return nil
		case got > want:
			e := out.Errorf("schema-newer", "%s was written by a newer shulker: its schema is v%d, and this shulker knows v%d", file, got, want)
			e.Nudge = out.Nudge{Lead: "Update shulker", Command: "shulker self update"}
			return e
		}
	}
	what := "names no $schema"
	if head.Schema != "" {
		what = "names the schema " + head.Schema
	}
	return Invalid(code, file, data, errors.New(what+", which this shulker doesn't know"))
}
