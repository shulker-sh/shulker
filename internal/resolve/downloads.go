package resolve

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

const DownloadsDir = "downloads"

type dropped struct {
	Name   string
	Sha512 string
	Sha1   string
}

func (r *Resolver) sweepDownloads() ([]dropped, error) {
	dir := filepath.Join(r.Dir, DownloadsDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []dropped
	for _, e := range entries {
		if !e.Type().IsRegular() || e.Name()[0] == '.' {
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
	return files, nil
}

func lockID(providerName, id string) any {
	if providerName == "curseforge" {
		if n, err := strconv.Atoi(id); err == nil {
			return n
		}
	}
	return id
}
