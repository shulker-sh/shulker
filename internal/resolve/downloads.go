package resolve

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"shulker.sh/shulker/internal/manual"
	"shulker.sh/shulker/internal/modpack"
)

const DownloadsDir = modpack.DownloadsDir

type dropped struct {
	Name   string
	Sha512 string
	Sha1   string
}

func (r *Resolver) downloads() string {
	if r.DownloadsIn != "" {
		return r.DownloadsIn
	}
	return filepath.Join(r.Dir, DownloadsDir)
}

func (r *Resolver) sweepDownloads() ([]dropped, error) {
	dir := r.downloads()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		r.droppedSums = map[string]bool{}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []dropped
	for _, e := range entries {
		if !e.Type().IsRegular() || e.Name()[0] == '.' || !slices.Contains(manual.Kinds, strings.ToLower(filepath.Ext(e.Name()))) {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		h := sha1.New()
		sha, err := r.Cache.Put(io.TeeReader(f, h))
		f.Close()
		if err != nil {
			return nil, err
		}
		files = append(files, dropped{Name: e.Name(), Sha512: sha, Sha1: hex.EncodeToString(h.Sum(nil))})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	r.droppedSums = map[string]bool{}
	for _, f := range files {
		r.droppedSums[f.Sha512] = true
	}
	return files, nil
}

// isDropped reports whether the object sha is a file in the downloads folder: a copy by hand, which
// says nothing of whether its provider's URL serves it. It reads the last sweep, sweeping only when
// none has run, since every cached file a provider hashes by sha1 alone asks.
func (r *Resolver) isDropped(sha string) (bool, error) {
	if r.droppedSums == nil {
		if _, err := r.sweepDownloads(); err != nil {
			return false, err
		}
	}
	return r.droppedSums[sha], nil
}
