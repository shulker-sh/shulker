package resolve

import (
	"bytes"
	"context"
	"encoding/json"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/project"
)

// RelockOptions shape a relock. With KeepUnchanged, a relock that changes nothing writes nothing,
// so a sync on every launch doesn't fill the history with copies. Reason is the command an
// in-place project's history entry names.
type RelockOptions struct {
	KeepUnchanged bool
	Reason        string
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
	// WasSaved reports whether the manifest and lock were written; a relock that keeps an
	// unchanged lock writes nothing.
	WasSaved bool
}

// Relock re-resolves p's lock with run and saves it: a modpack whose ref or path moved is resolved
// again, the lock is reconciled before and after run, validated, and its changes since before are
// taken. An in-place project keeps a history entry before the manifest and lock are rewritten,
// which is the state a rollback puts back. Nothing is saved when the lock is unchanged and not
// stale and opts say to keep it.
func (r *Resolver) Relock(ctx context.Context, store *pack.Store, p *project.Project, run func(*project.Project, *Resolver) (pin string, err error), opts RelockOptions) (Relocked, error) {
	if err := p.RequireLock(); err != nil {
		return Relocked{}, err
	}
	stale := p.IsLockStale()
	original, err := json.Marshal(p.Lock)
	if err != nil {
		return Relocked{}, err
	}
	before := r.Snapshot()
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
	if err := v.Err(); err != nil {
		return Relocked{}, err
	}
	placements := (&build.Builder{Manifest: r.Manifest, Lock: r.Lock, Packs: r.Packs}).Placements()
	rl := Relocked{Changes: r.Changes(before), Reresolved: reresolved, Pin: pin, Validation: v, Placements: placements}
	rl.Warnings = append(rl.Warnings, r.Warnings...)
	rl.Warnings = append(rl.Warnings, v.Warnings...)
	rl.Warnings = append(rl.Warnings, rl.Changes.Unshipped(p.Manifest.Sides(), r.Lock.Mods, placements)...)
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
func (r *Resolver) resolveMovedRefs(ctx context.Context, store *pack.Store) error {
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
