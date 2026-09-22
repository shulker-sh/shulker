package meta

import (
	"archive/zip"
	"encoding/json"
	"io"
	"sort"
)

type InstallerLibrary struct {
	Name string
	URL  string
	Sha1 string
}

// InstallerLibraries lists what a NeoForge or Forge installer jar downloads: the libraries in its
// install_profile.json and version.json. Libraries without a URL ship inside the installer itself.
func InstallerLibraries(path string) ([]InstallerLibrary, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, unreadable(err, "zip", "the loader installer %s can't be read", path)
	}
	defer zr.Close()
	byName := map[string]InstallerLibrary{}
	for _, name := range []string{"install_profile.json", "version.json"} {
		var profile struct {
			Libraries []struct {
				Name      string `json:"name"`
				Downloads struct {
					Artifact struct {
						URL  string `json:"url"`
						Sha1 string `json:"sha1"`
					} `json:"artifact"`
				} `json:"downloads"`
			} `json:"libraries"`
		}
		f, err := zr.Open(name)
		if err != nil {
			return nil, unreadable(err, "zip", "the loader installer %s can't be read", path)
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return nil, unreadable(err, "zip", "the loader installer %s can't be read", path)
		}
		if err := json.Unmarshal(data, &profile); err != nil {
			return nil, unreadable(err, name, "the loader installer %s can't be read", path)
		}
		for _, lib := range profile.Libraries {
			if a := lib.Downloads.Artifact; a.URL != "" {
				byName[lib.Name] = InstallerLibrary{Name: lib.Name, URL: a.URL, Sha1: a.Sha1}
			}
		}
	}
	libs := make([]InstallerLibrary, 0, len(byName))
	for _, lib := range byName {
		libs = append(libs, lib)
	}
	sort.Slice(libs, func(i, j int) bool { return libs[i].Name < libs[j].Name })
	return libs, nil
}

// InstallerVersion is the version.json a NeoForge or Forge installer writes into a launcher.
func InstallerVersion(path string) (json.RawMessage, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, unreadable(err, "zip", "the loader installer %s can't be read", path)
	}
	defer zr.Close()
	f, err := zr.Open("version.json")
	if err != nil {
		return nil, unreadable(err, "zip", "the loader installer %s can't be read", path)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, unreadable(err, "zip", "the loader installer %s can't be read", path)
	}
	if !json.Valid(data) {
		return nil, invalid("the loader installer %s holds a version.json that isn't valid JSON", path)
	}
	return data, nil
}
