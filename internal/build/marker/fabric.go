package marker

import "encoding/json"

// fabric is the Fabric and Quilt marker: fabric.mod.json, the icon, and the ModMenu entrypoint
// with the link labels it shows.
func fabric(info Info) ([]entry, error) {
	contact, links, labels := modMenuLinks(info.Links)
	meta := map[string]any{
		"schemaVersion": 1,
		"id":            info.ID,
		"version":       info.Version,
		"name":          info.Name,
		"description":   info.Description.render(quickText),
		"icon":          "assets/" + info.ID + "/icon.png",
		"environment":   "*",
		"entrypoints":   map[string]any{"modmenu": []string{modMenuEntrypoint}},
		"custom":        map[string]any{"modmenu": modMenuCustom(links)},
	}
	if len(info.Authors) > 0 {
		meta["authors"] = info.Authors
	}
	// Fabric, unlike FML, is content with a mod that names no license, so this one stays unset.
	if info.License != "" {
		meta["license"] = info.License
	}
	if len(contact) > 0 {
		meta["contact"] = contact
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	entries := []entry{
		{"fabric.mod.json", metaData},
		{"assets/" + info.ID + "/icon.png", Icon},
	}
	if len(labels) > 0 {
		lang, err := json.MarshalIndent(labels, "", "  ")
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{"assets/" + info.ID + "/lang/en_us.json", lang})
	}
	classes, err := modMenuClasses()
	if err != nil {
		return nil, err
	}
	return append(entries, classes...), nil
}
