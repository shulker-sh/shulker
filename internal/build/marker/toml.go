package marker

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/BurntSushi/toml"
	"shulker.sh/shulker/internal/loader"
)

// plainText is the description as the FML loaders show it, verbatim.
var plainText = style{}

type tomlMeta struct {
	ModLoader       string    `toml:"modLoader,omitempty"`
	LoaderVersion   string    `toml:"loaderVersion,omitempty"`
	License         string    `toml:"license"`
	LicenseURL      string    `toml:"licenseURL,omitempty"`
	IssueTrackerURL string    `toml:"issueTrackerURL,omitempty"`
	Mods            []tomlMod `toml:"mods"`
}

type tomlMod struct {
	ModID       string `toml:"modId"`
	Version     string `toml:"version"`
	DisplayName string `toml:"displayName"`
	LogoFile    string `toml:"logoFile"`
	IconFile    string `toml:"iconFile,omitempty"`
	IconBlur    bool   `toml:"iconBlur,omitempty"`
	Authors     string `toml:"authors,omitempty"`
	DisplayURL  string `toml:"displayURL,omitempty"`
	Description string `toml:"description"`
}

const (
	packFormat    = 15
	packFormatMax = 9999
)

// fml is the NeoForge and Forge marker, declared in mods.toml or neoforge.mods.toml: no classes,
// since both loaders load a mod that declares none, and a pack.mcmeta so Forge doesn't warn that
// the mod's pack metadata is missing.
func fml(l loader.Loader, info Info) ([]entry, error) {
	mod := tomlMod{
		ModID:       info.ID,
		Version:     info.Version,
		DisplayName: info.Name,
		LogoFile:    IconFile,
		Authors:     strings.Join(info.Authors, ", "),
		DisplayURL:  info.Links["website"],
		Description: info.Description.render(plainText),
	}
	meta := tomlMeta{
		ModLoader:       l.MarkerModLoader,
		License:         fmlLicense(info.License),
		LicenseURL:      info.Links["license"],
		IssueTrackerURL: info.Links["issues"],
	}
	if l.MarkerModLoader != "" {
		meta.LoaderVersion = "[1,)"
	}
	if l.MarkerIconFile {
		// iconBlur scales the 128px icon into the 24px slot smoothly rather than by nearest neighbour.
		mod.IconFile, mod.IconBlur = IconFile, true
	}
	meta.Mods = []tomlMod{mod}
	var metaData bytes.Buffer
	if err := toml.NewEncoder(&metaData).Encode(meta); err != nil {
		return nil, err
	}
	// The marker ships no assets or data; the pack metadata only exists so FML doesn't report it as
	// a mod with missing pack metadata, and it has to read as compatible or the game leaves the mod
	// out of its pack list. Both schemas are written because the field names changed: Minecraft
	// read `pack_format` with a `supported_formats` range until 26.x, which reads `min_format` and
	// `max_format` (26.2 is resource format 88, data format 107 — well past the old 1–99 range
	// this used to declare). The ranges say "whatever is running", since there is nothing to break.
	pack, err := json.MarshalIndent(map[string]any{"pack": map[string]any{
		"description":       info.Name,
		"pack_format":       packFormat,
		"supported_formats": map[string]int{"min_inclusive": 1, "max_inclusive": packFormatMax},
		"min_format":        []int{1, 0},
		"max_format":        packFormatMax,
	}}, "", "  ")
	if err != nil {
		return nil, err
	}
	return []entry{
		{l.MarkerFile, metaData.Bytes()},
		{"pack.mcmeta", pack},
		{IconFile, Icon},
	}, nil
}

// fmlLicense falls back rather than leaving the field empty, which both FML loaders read as a mod
// file declaring no license and refuse to load.
func fmlLicense(license string) string {
	if license == "" {
		return "All rights reserved"
	}
	return license
}
