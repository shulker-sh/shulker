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
