package project

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
)

// LinkSource is what a link makes an instance follow: the checkout it was read from, the name
// the instance's manifest and registry row record it under, and its project. An authored source
// is a link's answers rather than a checkout: a project with no directory yet, which the link
// writes into the instance it creates.
type LinkSource struct {
	*pack.Checkout
	Name     string
	Project  *Project
	IsAuthor bool
}

// LinkInstance is the project a link leaves in the game directory: the minimal manifest ADR 0001
// calls an instance, following the link's source as a modpack and building where it stands. A
// project already there is adopted, never replaced, so a relink keeps whatever the player added
// on top of the pack. Its name is set to the id either way, since repair reads the id back from it.
// linked names the modpack entry this link wrote or repointed, and is empty when it left them alone.
func LinkInstance(gameDir, id, display string, src *LinkSource) (p *Project, linked string, err error) {
	p, err = Open(gameDir)
	if src.IsAuthor {
		if err == nil {
			return nil, "", authoredOver(gameDir)
		}
		if !errors.Is(err, ErrNoManifest) {
			return nil, "", err
		}
		p, err := AuthorInstance(gameDir, id, display, src)
		return p, "", err
	}
	if errors.Is(err, ErrNoManifest) {
		p, err := NewInstance(gameDir, id, display, src)
		return p, src.Project.Manifest.Name, err
	}
	if err != nil {
		return nil, "", err
	}
	if p.Lock == nil {
		p.Lock = lock.New()
	}
	changed := false
	if p.Manifest.Name != id {
		p.Manifest.Name, changed = id, true
	}
	// A project that builds elsewhere is not yet an instance; linking it here is what makes it one.
	if !p.Manifest.BuildsInPlace("client") {
		if p.Manifest.Client == nil {
			p.Manifest.Client = &manifest.Client{Name: display}
		}
		p.Manifest.Client.Build, changed = ".", true
	}
	// Only --force reaches here with a source the instance doesn't follow yet. Repointing that one
	// modpack entry leaves the player's own requires, and the lock holding them, where they are.
	key := ModpackKey(p.Manifest, src.Name)
	if key == "" {
		key = src.Project.Manifest.Name
	}
	entry, held := p.Manifest.Requires[key]
	if held && entry.Kind() != manifest.TypeModpack {
		return nil, "", manifest.KeyTaken(key, entry.Kind(), manifest.TypeModpack)
	}
	if entry.Source != src.Name || entry.Ref != src.Ref || entry.Path != src.Path {
		entry.Source, entry.Ref, entry.Path = src.Name, src.Ref, src.Path
		p.Manifest.Requires[key], changed = entry, true
		linked = key
	}
	if !changed {
		return p, linked, nil
	}
	return p, linked, p.SaveManifest()
}

// NewInstance writes the instance manifest. It pins no platform and lists no feature: the pack is
// locked, so the relock inherits all of that, and a pack that moves platform is followed rather
// than fought. What it does copy is the two preferences only the pack's author can weigh, its
// history retention and whether builds carry the marker mod; from then on both are the player's.
func NewInstance(gameDir, id, display string, src *LinkSource) (*Project, error) {
	followed := src.Project.Manifest
	m := &manifest.Manifest{
		Schema:   manifest.SchemaURL,
		Name:     id,
		Requires: map[string]manifest.Require{followed.Name: {Source: src.Name, Ref: src.Ref, Path: src.Path}},
		Client:   &manifest.Client{Name: display, Build: "."},
	}
	if followed.History != nil {
		keep := *followed.History
		m.History = &keep
	}
	if followed.Marker != nil {
		marker := *followed.Marker
		m.Marker = &marker
	}
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		return nil, err
	}
	p := &Project{Dir: gameDir, Manifest: m, Lock: lock.New()}
	return p, p.SaveManifest()
}

// AuthorInstance writes the project the link's answers describe into the game directory, which
// from then on is both the instance and the project it builds. It follows nothing, so it syncs from
// itself, and that is the source its registry row records.
func AuthorInstance(gameDir, id, display string, src *LinkSource) (*Project, error) {
	m := *src.Project.Manifest
	client := *m.Client
	m.Name, client.Name, client.Build = id, display, "."
	m.Client = &client
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		return nil, err
	}
	p := &Project{Dir: gameDir, Manifest: &m, Lock: src.Project.Lock}
	if err := p.SaveManifest(); err != nil {
		return nil, err
	}
	if err := p.SaveLock(); err != nil {
		return nil, err
	}
	src.Name, src.Source, src.Dir = gameDir, gameDir, gameDir
	return p, nil
}

// authoredOver refuses to author an instance where a project already stands: there is no pack to
// repoint, so --force has nothing to do either, and the answers would only overwrite a player's
// own instance.
func authoredOver(gameDir string) error {
	e := out.Errorf("instance-exists", "%s already holds a project", gameDir)
	e.Help = "give the new instance another name"
	return e
}

// CheckAdopt guards the project a link is about to adopt. A game directory holding an in-place
// project keeps the pack it follows, so the source a link names has to agree with the manifest
// before the link may take it over, and force is what repoints that one entry. The manifest is
// what decides, not the registry row: an unlink deletes the row and leaves the project whole.
// noun, name and second word the refusal: what the launcher calls the instance, what this one is
// called, and the flag that would make a second one instead.
func CheckAdopt(gameDir string, src *LinkSource, noun, name, second string, force bool) error {
	m, _, inPlace, err := InPlace(gameDir)
	if err != nil || !inPlace {
		return err
	}
	if src.IsAuthor {
		return authoredOver(gameDir)
	}
	if force {
		return nil
	}
	key := ModpackKey(m, src.Name)
	if key == "" || (m.Requires[key].Source == src.Name && m.Requires[key].Path == src.Path) {
		return nil
	}
	e := out.Errorf("instance-exists", "%s %q already follows %s from %s", noun, name, key, m.Requires[key].Source)
	e.Help = fmt.Sprintf("pass %s to create a second %s, or --force to repoint the modpack it follows", second, noun)
	return e
}

// ModpackKey is the key an instance follows a link's source under: the source manifest's name
// when the link wrote the entry, and whatever an earlier link or a hand edit chose when it didn't.
// Empty where no single entry is the link's: with several packs required, none of them is the one.
func ModpackKey(m *manifest.Manifest, source string) string {
	keys := slices.Sorted(maps.Keys(m.Modpacks()))
	for _, key := range keys {
		if m.Requires[key].Source == source {
			return key
		}
	}
	if len(keys) == 1 {
		return keys[0]
	}
	return ""
}
