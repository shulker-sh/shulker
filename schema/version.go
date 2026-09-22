package schema

import (
	"encoding/json"
	"errors"
	"fmt"
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
	got, want, err := ReadMarker(kind, data)
	if err != nil {
		return Invalid(code, file, data, err)
	}
	if got > want {
		e := out.Errorf("schema-newer", "%s %s", file, Newer(got, want))
		e.Nudge = UpdateNudge
		return e
	}
	return nil
}

// UpdateNudge is the fix for a file written by a newer shulker, on the error or the warning that
// reports it.
var UpdateNudge = out.Nudge{Lead: "Update shulker", Command: "shulker self update"}

// Newer says a file was written by a newer shulker, naming both versions; the file's path goes
// before it.
func Newer(got, want int) string {
	return fmt.Sprintf("was written by a newer shulker (schema v%d; this one reads up to v%d)", got, want)
}

// ReadMarker returns the version data's $schema line names and the one this shulker knows for kind.
// A newer file is got > want with no error; a lower, foreign or absent marker, or data that isn't
// JSON, is an error saying why the file can't be read.
func ReadMarker(kind Kind, data []byte) (got, want int, err error) {
	want, ok := markerVersion(URL(kind), path.Base(string(kind)))
	if !ok {
		return 0, 0, errors.New("schema kind " + string(kind) + " has no version")
	}
	var head struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return 0, want, err
	}
	if got, ok := markerVersion(head.Schema, path.Base(string(kind))); ok && got >= want {
		return got, want, nil
	}
	what := "names no $schema"
	if head.Schema != "" {
		what = "names the schema " + head.Schema
	}
	return 0, want, errors.New(what + ", which this shulker doesn't know")
}
