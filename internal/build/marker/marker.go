// Package marker builds the marker mod: the jar that gives a pack its own entry in the in-game mod
// list and carries its manifest and lock, so an export of the build is recognised as the pack again.
// It takes plain Info and returns the jar, in the metadata format of the loader that reads it.
package marker

import (
	_ "embed"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/zipfile"
)

// Icon is the shulker icon the marker shows, and an export carries for a pack with none of its own.
//
//go:embed assets/icon.png
var Icon []byte

// IconFile is the name the icon goes by, in the jar's root and in an export.
const IconFile = "icon.png"

const modsPath = "shulker/mods.txt"

// Info is what the marker says about a pack.
type Info struct {
	ID      string
	Version string
	Name    string
	Authors []string
	License string
	// Links are the manifest's links, by label.
	Links       map[string]string
	Description Description
	// Direct are the placed mods the manifest lists; Deps the placed mods it doesn't.
	Direct, Deps []string
	// Files are carried in the jar as they are, by path.
	Files map[string][]byte
}

// Description is the marker's mod list description: Text, then Summary with its Labels beneath, then
// each Section with items.
type Description struct {
	Text     string
	Summary  string
	Labels   []Label
	Sections []Section
}

// Label is a named value that says how this build varies, such as the features it has on.
type Label struct{ Name, Value string }

// Section is a titled list in a Description, left out when it has no Items.
type Section struct {
	Title string
	Items []Item
}

// Item is a line of a Section, with an optional Note on why it is there.
type Item struct{ Text, Note string }

// ModID is the marker's mod id for the pack called name.
func ModID(name string) string {
	return "shulker_" + strings.NewReplacer(".", "_", "-", "_").Replace(name)
}

// Version is the marker's mod version for a pack at packVersion whose lock hashes to lockHash. It
// has to start with a digit: both FML loaders reject a mod whose version does not, so a manifest
// without a version gets a zero version with the lock hash as build metadata.
func Version(packVersion, lockHash string) string {
	build := lockHash[:8]
	if packVersion == "" {
		packVersion = "0.0.0"
	}
	return packVersion + "+" + build
}

// JarPath is where a build places the marker of the pack called name.
func JarPath(name string) string {
	return "mods/shulker-" + name + ".jar"
}

type entry struct {
	name string
	data []byte
}

// Jar is the marker jar for a build on l.
func Jar(l loader.Loader, info Info) ([]byte, error) {
	var entries []entry
	var err error
	// The marker declares itself in the file its loader reads, and that file decides the format.
	if strings.HasSuffix(l.MarkerFile, ".json") {
		entries, err = fabric(info)
	} else {
		entries, err = fml(l, info)
	}
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(entries)+len(info.Files)+1)
	for _, e := range entries {
		files[e.name] = e.data
	}
	for name, data := range info.Files {
		files[name] = data
	}
	files[modsPath] = modList(info.Direct, info.Deps)
	return zipfile.Build(files, "")
}

func modList(direct, deps []string) []byte {
	ids := slices.Sorted(slices.Values(slices.Concat(direct, deps)))
	return []byte(strings.Join(ids, "\n") + "\n")
}

// style renders a description for ModMenu, which parses it as QuickText, or as the plain text the
// FML loaders show verbatim.
type style struct{ isRich bool }

func (s style) tag(name, text string) string {
	if !s.isRich {
		return text
	}
	return "<" + name + ">" + text + "</" + name + ">"
}

func (s style) escape(text string) string {
	if !s.isRich {
		return text
	}
	return strings.ReplaceAll(text, "<", "\\<")
}

func (d Description) render(s style) string {
	var parts []string
	if d.Text != "" {
		parts = append(parts, s.escape(strings.TrimSpace(d.Text)))
	}
	summary := d.Summary
	if len(d.Labels) > 0 {
		labels := make([]string, len(d.Labels))
		for i, l := range d.Labels {
			labels[i] = s.tag("gray", s.tag("bold", l.Name+":")) + " " + l.Value
		}
		summary += "\n" + strings.Join(labels, " • ")
	}
	parts = append(parts, summary)
	for _, section := range d.Sections {
		if len(section.Items) == 0 {
			continue
		}
		lines := []string{s.tag("bold", section.Title)}
		for _, item := range section.Items {
			line := "  • " + item.Text
			if item.Note != "" {
				line += " " + s.tag("gray", "("+item.Note+")")
			}
			lines = append(lines, line)
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	return strings.Join(parts, "\n\n")
}
