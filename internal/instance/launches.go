package instance

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"shulker.sh/shulker/internal/fsutil"
)

const (
	LaunchesFileName     = "launches.json"
	DefaultLaunchHistory = 5

	OutcomeOK      = "ok"
	OutcomeCrashed = "crashed"
	// OutcomeNotStarted is a run the game never began: shulker could not run Java at all, so there
	// is no log, no crash report and no exit code to read, only the reason it could not.
	OutcomeNotStarted = "not-started"
)

// Launch is one run of the game: the pre-launch hook stamps it and the post-exit hook closes it.
// No launcher hands a post-exit command the game's exit code, so the outcome is read from a crash
// report newer than StartedAt. A run whose post-exit never fired keeps an open record, with no
// EndedAt and no Outcome. A run the game never began is closed on the spot as OutcomeNotStarted,
// with StartedAt and EndedAt the same moment, since none of the time that passed was the game's.
type Launch struct {
	StartedAt   string `json:"startedAt"`
	EndedAt     string `json:"endedAt,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	Log         string `json:"log,omitempty"`
	CrashReport string `json:"crashReport,omitempty"`
	// Error names the executable that would not run and the operating system's reason. It never
	// carries any part of the game's argv, which holds the session access token.
	Error string `json:"error,omitempty"`
}

func LaunchesPath(dir string) string { return filepath.Join(dir, Dir, LaunchesFileName) }

// LaunchKeep is how many launch records are kept, mirroring the manifest's history count: -1 keeps
// every record, 0 keeps none and records nothing at all.
func (s Settings) LaunchKeep() int {
	if s.LaunchHistory == nil {
		return DefaultLaunchHistory
	}
	return *s.LaunchHistory
}

// LoadLaunches reads the records oldest first. Nothing hand-edits this file and a hook must never
// abort a launch over it, so a missing or unreadable one reads as no records.
func LoadLaunches(dir string) []Launch {
	data, err := os.ReadFile(LaunchesPath(dir))
	if err != nil {
		return nil
	}
	var records []Launch
	if json.Unmarshal(data, &records) != nil {
		return nil
	}
	return records
}

// SaveLaunches writes the newest keep records. A keep of 0 removes the file, so turning the cap off
// leaves nothing behind.
func SaveLaunches(dir string, records []Launch, keep int) error {
	if keep == 0 {
		if err := os.Remove(LaunchesPath(dir)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if keep > 0 && len(records) > keep {
		records = records[len(records)-keep:]
	}
	if err := os.MkdirAll(filepath.Join(dir, Dir), 0o755); err != nil {
		return err
	}
	return fsutil.WriteJSON(LaunchesPath(dir), records)
}
