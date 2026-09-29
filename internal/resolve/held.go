package resolve

import (
	"context"
	"fmt"
	"strings"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// heldMods is what the lock held before an add ran, so the add can tell a
// version it kept from one it placed itself.
type heldMods map[string]lock.Mod

func holdVersions(l *lock.Lock) heldMods {
	held := make(heldMods, len(l.Mods))
	for id, m := range l.Mods {
		held[id] = m
	}
	return held
}

// heldMove is a dependency that can only be satisfied by moving a version the
// lock already held.
type heldMove struct {
	mod        string
	modVersion string
	dep        string
	declared   string
	found      string
	modpack    string
}

func (m heldMove) line() string {
	return fmt.Sprintf("%s %s requires %s %s, but %s is held at %s", m.mod, m.modVersion, m.dep, m.declared, m.dep, m.found)
}

// settleHeld is the partial-update half of add: every version the lock held
// stays put, and a dependency that needs one of them to move stops the add
// unless --with-deps allows it.
func (r *Resolver) settleHeld(ctx context.Context, held heldMods, p provider.Provider, v *provider.Version, added, channel string, withDeps bool) error {
	moves, err := r.heldMoves(held, r.touchedBy(held, added))
	if err != nil || len(moves) == 0 {
		return err
	}
	if !withDeps {
		held := heldError(moves, added)
		if r.AskMove == nil {
			return held
		}
		move, err := r.AskMove(held)
		if err != nil {
			return err
		}
		if !move {
			return held
		}
	}
	for _, m := range moves {
		r.log("moving %s %s, which %s needs another version of", m.dep, m.found, m.mod)
		if m.modpack != "" {
			r.list(m.dep)
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s is now listed in shulker.json, so it no longer follows modpack %s.", m.dep, m.modpack))
		}
		r.dropLocked(m.dep)
	}
	direct := r.directMods()
	for _, m := range moves {
		if d, isDirect := direct[m.dep]; isDirect && d.locked == "" {
			if err := r.relock(ctx, m.dep, lock.Mod{}); err != nil {
				return err
			}
		}
	}
	return r.addDeps(ctx, p, v, added, channel, map[string]bool{})
}

// list writes a manifest entry for a mod a locked modpack provided, recording
// where it came from. Without it applyLockedPacks would copy the modpack's
// version straight back over the move on the next reconcile.
func (r *Resolver) list(id string) {
	m := r.Lock.Mods[id]
	entry := manifest.Require{Project: m.Project}
	if m.Provider != "" && m.Provider != r.Manifest.ProviderOrder()[0] {
		entry.Provider = m.Provider
	}
	r.Manifest.Requires[id] = entry
}

// touchedBy lists the mods this add is answerable for: the one named, and every
// entry the lock did not already hold. A dependency kept at the version it was
// already locked at is left out, so a problem that predates the add is not one
// the add is asked to fix.
func (r *Resolver) touchedBy(held heldMods, added string) map[string]bool {
	touched := map[string]bool{added: true}
	for _, id := range r.lockIDs() {
		if _, was := held[id]; !was {
			touched[id] = true
		}
	}
	return touched
}

// heldMoves reads the jars: a dependency the add cannot satisfy is one the
// existing validation reports against a mod the add touched, where what it
// depends on is an entry the lock held and this add left alone.
func (r *Resolver) heldMoves(held heldMods, touched map[string]bool) ([]heldMove, error) {
	v, err := r.Validate()
	if err != nil {
		return nil, err
	}
	var moves []heldMove
	for _, p := range v.Problems {
		if p.Rule != "depends" || p.Found == "" || !touched[p.Mod] {
			continue
		}
		key := r.keyFor(p.On)
		if key == "" || touched[key] {
			continue
		}
		was, wasHeld := held[key]
		now, stillLocked := r.Lock.Mods[key]
		if !wasHeld || !stillLocked || was.Sha512 != now.Sha512 {
			continue
		}
		moves = append(moves, heldMove{mod: p.Mod, modVersion: p.ModVersion, dep: key, declared: p.Declared, found: p.Found, modpack: now.Modpack})
	}
	return moves, nil
}

// keyFor finds the lock entry a declared dependency names. An id that no entry
// owns is one a loader or another mod provides, which no move can change.
func (r *Resolver) keyFor(jarID string) string {
	for _, key := range r.lockIDs() {
		if r.Lock.JarID(key) == jarID {
			return key
		}
	}
	return ""
}

func heldError(moves []heldMove, added string) *out.Error {
	items := make([]string, 0, len(moves))
	rows := make([]out.Detail, 0, len(moves))
	for _, m := range moves {
		items = append(items, m.line())
		rows = append(rows, out.Detail{Text: m.line()})
	}
	lead := "Move it"
	if len(moves) > 1 {
		lead = "Move them"
	}
	e := out.Errorf("deps-held", "%s needs a version the lock holds:\n  - %s", added, strings.Join(items, "\n  - "))
	e.Items = items
	e.Rows = rows
	e.Nudge = out.Nudge{Lead: lead, Command: "shulker add " + added + " --with-deps"}
	return e
}
