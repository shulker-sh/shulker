package marker

import (
	"embed"
	"io/fs"
	"strings"
)

//go:generate sh modmenu/compile.sh

//go:embed modmenu/classes
var modMenuClassFiles embed.FS

const (
	modMenuClassesDir = "modmenu/classes"
	modMenuEntrypoint = "shulker.marker.ShulkerModMenu"
)

// quickText is ModMenu's description markup.
var quickText = style{isRich: true}

func modMenuClasses() ([]entry, error) {
	var entries []entry
	err := fs.WalkDir(modMenuClassFiles, modMenuClassesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := modMenuClassFiles.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, entry{strings.TrimPrefix(path, modMenuClassesDir+"/"), data})
		return nil
	})
	return entries, err
}

// modMenuCustom is fabric.mod.json's custom.modmenu. The update checker is off, since the marker is
// on no provider to check.
func modMenuCustom(links map[string]string) map[string]any {
	custom := map[string]any{"update_checker": false}
	if len(links) > 0 {
		custom["links"] = links
	}
	return custom
}

var contactKeys = map[string]string{"website": "homepage", "issues": "issues", "source": "sources"}

var modMenuKnownLinks = map[string]bool{
	"buymeacoffee": true, "coindrop": true, "crowdin": true, "curseforge": true, "discord": true,
	"donate": true, "flattr": true, "github_releases": true, "github_sponsors": true, "kofi": true,
	"liberapay": true, "mastodon": true, "modrinth": true, "opencollective": true, "patreon": true,
	"paypal": true, "reddit": true, "twitch": true, "twitter": true, "wiki": true, "youtube": true,
}

// modMenuLinks sorts a manifest's links into fabric.mod.json's contact, ModMenu's links by their
// label key, and the labels of the ones ModMenu doesn't know, for the lang file.
func modMenuLinks(links map[string]string) (contact, modmenu, labels map[string]string) {
	contact, modmenu, labels = map[string]string{}, map[string]string{}, map[string]string{}
	for label, url := range links {
		if key, ok := contactKeys[label]; ok {
			contact[key] = url
			continue
		}
		if modMenuKnownLinks[label] {
			modmenu["modmenu."+label] = url
			continue
		}
		key := "shulker.link." + langKey(label)
		modmenu[key] = url
		labels[key] = label
	}
	return contact, modmenu, labels
}

func langKey(label string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(label) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('_')
		}
	}
	return sb.String()
}
