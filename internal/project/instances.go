package project

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

// InstanceEntry is an instance as shulker lists it: its registry row, and what its directory says
// about itself.
type InstanceEntry struct {
	config.Instance
	Status        string `json:"status"`
	SyncedAt      string `json:"syncedAt,omitempty"`
	Side          string `json:"side,omitempty"`
	AssumesClient bool   `json:"assumeClient,omitempty"`
	Ref           string `json:"ref,omitempty"`
	Path          string `json:"path,omitempty"`
	Problem       string `json:"problem,omitempty"`
	LaunchError   string `json:"launchError,omitempty"`
	// Detached marks a build with no registry row, found by the directory it was synced into.
	Detached bool `json:"-"`
}

// The statuses an entry carries: its directory is gone, can't be read, has never been built, or
// holds a build.
const (
	StatusSynced     = "synced"
	StatusNotSynced  = "not-synced"
	StatusMissing    = "missing"
	StatusUnreadable = "unreadable"
)

// Inspect reads what the instance directory says about itself: its manifest where it is a
// project, its instance file otherwise, and what the last build recorded where there is neither. A
// directory shulker can't read is still listed: `instances repair` is what fixes it.
func Inspect(in config.Instance) InstanceEntry {
	e := InstanceEntry{Instance: in, Status: StatusSynced}
	if _, err := os.Stat(in.Dir); errors.Is(err, os.ErrNotExist) {
		e.Status = StatusMissing
		return e
	} else if err != nil {
		e.Status = StatusUnreadable
		return e
	}
	hasIntent := false
	switch f, err := instance.Load(in.Dir); {
	case errors.Is(err, instance.ErrNotFound):
		e.Problem = "no " + filepath.Join(instance.Dir, instance.FileName)
	case err != nil:
		e.Problem = out.AsError(err).Message
	default:
		hasIntent = true
		e.Side, e.Ref, e.Path, e.AssumesClient = f.Side, f.Ref, f.Path, f.AssumesClient
		if e.Source == "" {
			e.Source = f.Source
		}
	}
	if records := instance.LoadLaunches(in.Dir); len(records) > 0 {
		if last := records[len(records)-1]; last.Outcome == instance.OutcomeNotStarted {
			e.LaunchError = last.Error
		}
	}
	pack, side, inPlace := InPlaceIntent(in.Dir)
	state, _ := build.ReadState(in.Dir)
	switch {
	case inPlace:
		e.Ref, e.Path, e.Side = pack.Ref, pack.Path, side
		if pack.Source != "" {
			e.Source = pack.Source
		}
	case !hasIntent:
		e.Ref, e.Path = state.Ref, state.Path
		if e.Source == "" {
			e.Source = state.Source
		}
	}
	if _, err := os.Stat(build.StatePath(in.Dir)); errors.Is(err, os.ErrNotExist) {
		e.Status = StatusNotSynced
		return e
	} else if err != nil {
		e.Status = StatusUnreadable
		return e
	}
	e.SyncedAt = state.BuiltAt
	if e.Side == "" {
		e.Side = state.Side
	}
	return e
}

// SortInstances orders entries for display: launchers in the launcher table's order, then by
// launcher name, label and directory.
func SortInstances(entries []InstanceEntry) {
	slices.SortStableFunc(entries, compareInstances)
}

func compareInstances(x, y InstanceEntry) int {
	if d := launcher.Rank(x.Launcher) - launcher.Rank(y.Launcher); d != 0 {
		return d
	}
	if c := strings.Compare(x.Launcher, y.Launcher); c != 0 {
		return c
	}
	if c := strings.Compare(strings.ToLower(x.Label()), strings.ToLower(y.Label())); c != 0 {
		return c
	}
	return strings.Compare(x.Dir, y.Dir)
}

// InstanceAt recognises a directory shulker syncs from what it holds, in order: an in-place
// shulker.json, the manifest of a project that is an instance under ADR 0001, whose one modpack
// entry says what it follows; the instance file's source; the state a build left before instance
// files existed. An instance file saying unlinked wins over all three, because unlink is what
// forgets. The name is the folder's until a launcher scan reads the one the launcher shows.
// lastSyncAt is when the directory was last built correctly, which a failure after that doesn't
// undo, so it is carried whatever the last sync did. lastError isn't: it belongs to the row shulker
// is replacing.
func InstanceAt(dir string) (config.Instance, bool) {
	f, err := instance.Load(dir)
	if err == nil && f.IsUnlinked {
		return config.Instance{}, false
	}
	// With several modpacks required none of them is the one the instance was linked from, so the
	// manifest says nothing and the sources below answer instead. A directory with no source
	// anywhere is no row shulker can write: the registry needs one.
	pack, _, _ := InPlaceIntent(dir)
	source := pack.Source
	if source == "" && err == nil {
		source = f.Source
	}
	if source == "" {
		st, _ := build.ReadState(dir)
		source = st.Source
	}
	if source == "" {
		return config.Instance{}, false
	}
	in := config.Instance{Name: filepath.Base(dir), Dir: dir, Source: source}
	if err == nil && f.Resolved != nil {
		in.LastSync = f.Resolved.LastSyncAt
	}
	return in, true
}

// Repaired is what RepairIntent did to an instance file: whether it wrote one, and when it wrote
// over a file it couldn't read, where the old one was kept and why.
type Repaired struct {
	Wrote      bool
	Kept       string
	Unreadable error
}

// RepairIntent writes the instance file for a directory shulker synced before it kept one, from
// what the build recorded. An instance that is a project gets a defaults-only file: its manifest
// holds what it follows, so anything written here could only go stale against it. A file it can't
// read is kept as instance.json.replaced.
func RepairIntent(in config.Instance) (Repaired, error) {
	_, loadErr := instance.Load(in.Dir)
	if loadErr == nil {
		return Repaired{}, nil
	}
	f := instance.New()
	if _, _, inPlace := InPlaceIntent(in.Dir); !inPlace {
		st, _ := build.ReadState(in.Dir)
		source := st.Source
		if source == "" {
			source = in.Source
		}
		if source == "" {
			return Repaired{}, nil
		}
		f.Source, f.Ref, f.Path, f.Side = source, st.Ref, st.Path, st.Side
	}
	kept, err := f.Replace(in.Dir)
	rep := Repaired{Wrote: true, Kept: kept}
	if kept != "" {
		rep.Unreadable = loadErr
	}
	return rep, err
}

// DetachedBuild is the directory as an entry when it holds a detached build: a sourced instance
// file, no manifest of its own and no registry row. Its id is slugged from the folder, since no
// row gave it one.
func DetachedBuild(dir string) (InstanceEntry, bool) {
	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
		return InstanceEntry{}, false
	}
	f, err := instance.Load(dir)
	if err != nil || f.Source == "" || f.IsUnlinked {
		return InstanceEntry{}, false
	}
	e := Inspect(config.Instance{Name: filepath.Base(dir), Dir: dir})
	e.ID = config.SlugID(e.Name)
	e.Detached = true
	return e, true
}
