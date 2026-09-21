package game

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
)

// Fetch downloads one file into the store and checks it against the hash the version JSON gave,
// landing it by rename so an interrupted run leaves nothing half-written behind.
func (s Store) Fetch(ctx context.Context, c *fetch.Client, f File) error {
	if f.URL == "" {
		return out.Errorf("store-incomplete", "%s isn't in the game store and the version json gives no source to fetch it from", f.Path)
	}
	path := s.Local(f)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha1.New()
	if _, err := c.Download(ctx, f.URL, io.MultiWriter(tmp, h)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if sum := hex.EncodeToString(h.Sum(nil)); f.Sha1 != "" && !strings.EqualFold(sum, f.Sha1) {
		e := out.Errorf("checksum-mismatch", "the download of %s doesn't match the sha1 its version json gives", f.Path)
		e.Rows = []out.Detail{{Label: "want", Text: f.Sha1}, {Label: "got", Text: sum}}
		return e
	}
	return os.Rename(tmp.Name(), path)
}

// AssetIndexFile lists every asset a version loads, keyed by the name the game asks for.
type AssetIndexFile struct {
	Objects map[string]AssetObject `json:"objects"`
}

type AssetObject struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// AssetFiles reads an asset index out of the store and returns the objects it names. They are
// stored by hash, so two versions naming the same asset share one file.
func (s Store) AssetFiles(indexID string) ([]File, error) {
	data, err := os.ReadFile(s.AssetIndex(indexID))
	if err != nil {
		return nil, err
	}
	var index AssetIndexFile
	if err := json.Unmarshal(data, &index); err != nil {
		e := out.Errorf("store-incomplete", "shulker can't parse %s", s.AssetIndex(indexID))
		e.Rows = []out.Detail{{Label: "json", Text: err.Error()}}
		return nil, e
	}
	base := s.Resources
	if base == "" {
		base = MojangResources
	}
	files := make([]File, 0, len(index.Objects))
	seen := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(index.Objects)) {
		o := index.Objects[name]
		if len(o.Hash) < 2 || seen[o.Hash] {
			continue
		}
		seen[o.Hash] = true
		files = append(files, File{
			Path: "assets/objects/" + o.Hash[:2] + "/" + o.Hash,
			URL:  strings.TrimSuffix(base, "/") + "/" + o.Hash[:2] + "/" + o.Hash,
			Sha1: o.Hash,
			Size: o.Size,
		})
	}
	return files, nil
}

// MojangResources is where an asset object is fetched from, by the first two characters of its
// hash and then the hash itself.
const MojangResources = "https://resources.download.minecraft.net/"

func sha1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
