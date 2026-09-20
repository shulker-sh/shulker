package account

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/schema"
)

const (
	FileName = "accounts.json"
	// Mode is what accounts.json is created with: it holds refresh and session tokens in plain
	// JSON, so nobody but its owner reads it.
	Mode fs.FileMode = 0o600
)

// Store is accounts.json: the accounts shulker signed in or created itself.
type Store struct {
	Schema   string    `json:"$schema"`
	Accounts []Account `json:"accounts,omitempty"`
}

// Path is accounts.json beside config.json.
func Path(configPath string) string { return filepath.Join(filepath.Dir(configPath), FileName) }

// Load reads the store. A file that isn't there yet is an empty store, the way an unlinked registry
// is; anything shulker can't read is accounts-invalid, beside config-invalid and registry-invalid.
func Load(path string) (Store, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Store{}, nil
	}
	if err != nil {
		return Store{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return Store{}, nil
	}
	if err := schema.CheckMarker(schema.Accounts, "accounts-invalid", path, data); err != nil {
		return Store{}, err
	}
	if err := schema.Validate(schema.Accounts, data); err != nil {
		return Store{}, schema.Invalid("accounts-invalid", path, data, err)
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return Store{}, schema.Invalid("accounts-invalid", path, data, err)
	}
	return s, nil
}

// Save writes the store with its $schema line, creating a missing file readable by its owner only.
// fsutil keeps the mode of a file that is already there.
func Save(path string, s Store) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	created := false
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, Mode)
	switch {
	case err == nil:
		f.Close()
		created = true
	case !errors.Is(err, fs.ErrExist):
		return err
	}
	s.Schema = schema.URL(schema.Accounts)
	if err := fsutil.WriteJSON(path, s); err != nil {
		if created {
			os.Remove(path)
		}
		return err
	}
	return nil
}

// Put stores an account, replacing the entry that is already this account: the one with the same
// id, or failing that the one with the same Xbox user hash. The hash is what an account that has
// just bought Java has in common with the entry it was stored under while it had no player UUID,
// and that id changes from the Xbox user id to the UUID underneath it.
func (s *Store) Put(a Account) {
	for i, have := range s.Accounts {
		if SameID(have.ID(), a.ID()) || sameUser(have, a) {
			s.Accounts[i] = a
			return
		}
	}
	s.Accounts = append(s.Accounts, a)
}

func sameUser(a, b Account) bool {
	return a.Xbox != nil && b.Xbox != nil && a.Xbox.UserHash != "" && a.Xbox.UserHash == b.Xbox.UserHash
}

// Remove drops the account with an id, and says whether it was there at all.
func (s *Store) Remove(id string) bool {
	for i, have := range s.Accounts {
		if SameID(have.ID(), id) {
			s.Accounts = append(s.Accounts[:i], s.Accounts[i+1:]...)
			return true
		}
	}
	return false
}
