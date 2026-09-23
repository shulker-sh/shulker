package manifest

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// ID is a provider's project or version id. CurseForge ids are integers on disk
// and every other provider's are strings; an ID keeps the token it was read or
// made as, so an all-digit Modrinth id stays a string.
type ID struct {
	s     string
	isNum bool
}

// NewID is providerName's id: an integer for CurseForge, a string otherwise.
func NewID(providerName, id string) ID {
	_, err := strconv.Atoi(id)
	return ID{s: id, isNum: providerName == "curseforge" && err == nil}
}

// String is the id as the provider writes it.
func (id ID) String() string { return id.s }

// For is the id as providerName writes it on disk, whichever token it was read from, so an id typed
// as a string still equals the integer the lock records for CurseForge.
func (id ID) For(providerName string) ID {
	if id.IsZero() {
		return id
	}
	return NewID(providerName, id.s)
}

// Int is the id as a number, for a CurseForge id.
func (id ID) Int() (int, bool) {
	n, err := strconv.Atoi(id.s)
	return n, err == nil
}

// IsZero reports whether the id is unset.
func (id ID) IsZero() bool { return id.s == "" }

// MarshalJSON writes the id as an integer or a string, the token it was read or made as.
func (id ID) MarshalJSON() ([]byte, error) {
	if id.isNum {
		return []byte(id.s), nil
	}
	return json.Marshal(id.s)
}

// UnmarshalJSON reads a string or an integer id.
func (id *ID) UnmarshalJSON(data []byte) error {
	if bytes.HasPrefix(data, []byte(`"`)) {
		*id = ID{}
		return json.Unmarshal(data, &id.s)
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*id = ID{s: n.String(), isNum: true}
	return nil
}
