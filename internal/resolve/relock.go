package resolve

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"slices"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

// RelockOptions shape a relock. With KeepUnchanged, a relock that changes nothing writes nothing,
// so a sync on every launch doesn't fill the history with copies. Reason is the command an
// in-place project's history entry names.
type RelockOptions struct {
	KeepUnchanged bool
	Reason        string
	// DropsFailing keeps the entries run added that validate: those a validation problem traces
	// back to come out again, and their problems are Relocked.Dropped.
	DropsFailing bool
}

// Relocked is what a relock changed, and what it found on the way.
type Relocked struct {
	Changes    *Changes
	Reresolved []string
	Pin        string
	Validation *Validation
	// Placements is where each locked mod and pack lands, by key, once the lock is relocked.
	Placements map[string]build.Placement
	// Warnings are the resolver's, the validation's, the mods that ship on no side, and the
	// history entry's, in the order they are raised.
	Warnings []string
	// SecurityWarnings are what security.minReleaseAge held back, and the young pins it took.
	SecurityWarnings []out.SecurityWarning
	// WasSaved reports whether the manifest and lock were written; a relock that keeps an
	// unchanged lock writes nothing.
	WasSaved bool
	// Dropped is the validation error of the entries a DropsFailing relock took out again.
	Dropped error
}

// Relock re-resolves p's lock with run and saves it: a modpack whose ref or path moved is resolved
// again, the lock is reconciled before and after run, validated, and its changes since before are
// taken. An in-place project keeps a history entry before the manifest and lock are rewritten,
// which is the state a rollback puts back. Nothing is saved when the lock is unchanged and not
// stale and opts say to keep it.
func (r *Resolver) Relock(ctx context.Context, store *modpack.Store, p *project.Project, run func(*project.Project, *Resolver) (pin string, err error), opts RelockOptions) (Relocked, error) {
	if err := p.RequireLock(); err != nil {
		return Relocked{}, err
	}
	stale := p.IsLockStale()
	original, err := json.Marshal(p.Lock)
	if err != nil {
		return Relocked{}, err
	}
	before := r.Snapshot()
	r.holdFloors(r.Lock)
	if err := r.resolveMovedRefs(ctx, store); err != nil {
		return Relocked{}, err
	}
	reresolved, err := r.Reconcile(ctx)
	if err != nil {
		return Relocked{}, err
	}
	if reresolved == nil {
		reresolved = []string{}
	}
	had := maps.Clone(r.Manifest.Requires)
	pin, err := run(p, r)
	if err != nil {
		return Relocked{}, err
	}
	if _, err := r.Reconcile(ctx); err != nil {
		return Relocked{}, err
	}
	v, err := r.Validate()
	if err != nil {
		return Relocked{}, err
	}
	var dropped error
	if err := v.Err(); err != nil {
		if !opts.DropsFailing {
			return Relocked{}, err
		}
		if v, dropped, err = r.dropFailing(ctx, v, addedKeys(had, r.Manifest.Requires)); err != nil {
			return Relocked{}, err
		}
	}
	placements := (&build.Builder{Manifest: r.Manifest, Lock: r.Lock, Packs: r.Packs}).Placements()
	rl := Relocked{Changes: r.Changes(before), Reresolved: reresolved, Pin: pin, Validation: v, Placements: placements, Dropped: dropped}
	rl.Warnings = append(rl.Warnings, r.Warnings...)
	rl.Warnings = append(rl.Warnings, v.Warnings...)
	rl.Warnings = append(rl.Warnings, rl.Changes.Unshipped(p.Manifest.Sides(), r.Lock.Mods, placements)...)
	rl.SecurityWarnings = r.AgeWarnings()
	if opts.KeepUnchanged && !stale {
		now, err := json.Marshal(p.Lock)
		if err != nil {
			return Relocked{}, err
		}
		if bytes.Equal(original, now) {
			return rl, nil
		}
	}
	if side, ok := p.Manifest.InPlaceSide(); ok {
		keep := p.Manifest.HistoryKeep()
		if _, err := build.TakeHistory(p.Dir, keep, build.HistoryEntry{Side: side, Reason: opts.Reason}); err != nil {
			return Relocked{}, err
		}
		warning, err := build.HistoryWarning(p.Dir, keep)
		if err != nil {
			return Relocked{}, err
		}
		if warning != "" {
			rl.Warnings = append(rl.Warnings, warning)
		}
	}
	if err := p.SaveManifest(); err != nil {
		return Relocked{}, err
	}
	if err := p.SaveLock(); err != nil {
		return Relocked{}, err
	}
	rl.WasSaved = true
	return rl, nil
}

// resolveMovedRefs resolves again each modpack whose ref or path moved under the same source, so
// the relock reads it where the manifest now points.
func (r *Resolver) resolveMovedRefs(ctx context.Context, store *modpack.Store) error {
	modpacks := r.Manifest.Modpacks()
	for i, l := range r.Packs {
		mp := modpacks[l.Name]
		pinned, locked := r.Lock.Modpacks[l.Name]
		if !locked || pinned.Source != mp.Source || (pinned.Ref == mp.Ref && pinned.Path == mp.Path) {
			continue
		}
		loaded, err := store.Resolve(ctx, l.Name, mp)
		if err != nil {
			return err
		}
		r.Packs[i] = loaded
	}
	return nil
}

func addedKeys[V any](before, after map[string]V) []string {
	var added []string
	for key := range after {
		if _, had := before[key]; !had {
			added = append(added, key)
		}
	}
	slices.Sort(added)
	return added
}

// dropFailing takes out the added entries v's problems trace back to, through the mods that
// require each other, and validates what is left. It returns v's problems as they were when a
// problem traces to nothing added, when every added entry fails, or when what is left still
// fails; otherwise the validation of what is left, and v's problems as the dropped error.
func (r *Resolver) dropFailing(ctx context.Context, v *Validation, added []string) (rest *Validation, dropped, err error) {
	failed := map[string]bool{}
	for _, p := range v.Problems {
		key, ok := r.addedAncestor(p.Mod, added)
		if !ok {
			return nil, nil, v.Err()
		}
		failed[key] = true
	}
	if len(failed) == len(added) {
		return nil, nil, v.Err()
	}
	if err := r.Remove(slices.Sorted(maps.Keys(failed))); err != nil {
		return nil, nil, err
	}
	if _, err := r.Reconcile(ctx); err != nil {
		return nil, nil, err
	}
	if rest, err = r.Validate(); err != nil {
		return nil, nil, err
	}
	if rest.Err() != nil {
		return nil, nil, v.Err()
	}
	return rest, v.Err(), nil
}

// addedAncestor is the added entry id is, or the first one found among the mods that require it.
func (r *Resolver) addedAncestor(id string, added []string) (string, bool) {
	seen := map[string]bool{}
	queue := []string{id}
	for len(queue) > 0 {
		id, queue = queue[0], queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		if slices.Contains(added, id) {
			return id, true
		}
		if m, ok := r.Lock.Mods[id]; ok {
			queue = append(queue, m.RequiredBy...)
		}
	}
	return "", false
}
