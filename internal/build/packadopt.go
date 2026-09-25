package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/integrations"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
)

// requiredResourcePacks are the packs the game or a loader always loads, which an enabled list
// names but client.resourcePacks never does.
var requiredResourcePacks = []string{"vanilla", "mod_resources", "fabric"}

// AdoptPackChoices reads the enabled resource packs and the shader an imported pack's client
// overrides ship into client.resourcePacks and client.shader, and takes those keys out of the
// override files, so the manifest is the one place they are set. A shipped name no imported pack is
// placed under is dropped with a warning. A list naming a pack a mod provides, which
// client.resourcePacks can't name, is left in the overrides as it is.
func AdoptPackChoices(m *manifest.Manifest, lk *lock.Lock, overrides []packarchive.Override) ([]packarchive.Override, []string) {
	if m.Client == nil {
		return overrides, nil
	}
	var warnings []string
	for i, o := range overrides {
		if o.Layer == packarchive.LayerFor("server") {
			continue
		}
		var adopted bool
		var key string
		var warned []string
		switch {
		case o.Path == OptionsFile:
			key = resourcePacksKey
			adopted, warned = adoptPackList(m, listFormFor(lk.Minecraft), o)
		case slices.ContainsFunc(integrations.Shaders, func(s integrations.Shader) bool { return s.Config != "" && s.Config == o.Path }):
			key = "shaderPack"
			adopted, warned = adoptShader(m, o)
		}
		warnings = append(warnings, warned...)
		if adopted {
			overrides[i].Data = withoutKey(o.Data, key)
		}
	}
	return overrides, warnings
}

// adoptPackList sets client.resourcePacks from the list o ships, reporting whether it did.
func adoptPackList(m *manifest.Manifest, form packForm, o packarchive.Override) (bool, []string) {
	list, ok := parseProperties(o.Data)[resourcePacksKey]
	if !ok {
		return false, nil
	}
	chosen, dropped, ok := chosenFromList(m, form, list)
	if !ok {
		return false, nil
	}
	m.Client.ResourcePacks = &chosen
	var warnings []string
	for _, name := range dropped {
		warnings = append(warnings, fmt.Sprintf("%s enables %s, which the pack doesn't place; left out of client.resourcePacks", o.Path, name))
	}
	return true, warnings
}

// adoptShader sets client.shader from the shaderPack o selects, reporting whether it did. One no
// shader is placed under leaves none selected.
func adoptShader(m *manifest.Manifest, o packarchive.Override) (bool, []string) {
	file, ok := parseProperties(o.Data)["shaderPack"]
	if !ok {
		return false, nil
	}
	key, found := packKeyByFile(m.Shaders(), file)
	m.Client.Shader = &key
	if file != "" && !found {
		return true, []string{fmt.Sprintf("%s selects %s, which the pack doesn't place; no shader is selected", o.Path, file)}
	}
	return true, nil
}

// chosenFromList turns an options.txt enabled list, bottom first, into client.resourcePacks, top
// first, with the file names no pack is placed under. It is false for a list naming a pack
// neither a file nor one of the game's own.
func chosenFromList(m *manifest.Manifest, form packForm, list string) (chosen, dropped []string, ok bool) {
	var entries []string
	if json.Unmarshal([]byte(list), &entries) != nil {
		return nil, nil, false
	}
	packs := m.PlacedAsResourcePacks()
	chosen = []string{}
	for _, entry := range slices.Backward(entries) {
		switch name, isFile := form.name(entry); {
		case slices.Contains(requiredResourcePacks, entry):
		case slices.Contains(manifest.BuiltinResourcePacks, entry):
			chosen = append(chosen, entry)
		case isFile && (form.prefix != "" || strings.HasSuffix(name, ".zip")):
			if key, found := packKeyByFile(packs, name); found {
				chosen = append(chosen, key)
			} else {
				dropped = append(dropped, name)
			}
		default:
			return nil, nil, false
		}
	}
	return chosen, dropped, true
}

// packKeyByFile is the key of the entry placed as file.
func packKeyByFile(entries map[string]manifest.Require, file string) (string, bool) {
	for key, r := range entries {
		if manifest.PackFilename(key, r) == file {
			return key, true
		}
	}
	return "", false
}

// withoutKey is a properties file with every line setting key taken out.
func withoutKey(data []byte, key string) []byte {
	var kept []string
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if k, _, ok := splitProperty(line); ok && k == key {
			continue
		}
		kept = append(kept, line)
	}
	return []byte(strings.Join(kept, ""))
}
