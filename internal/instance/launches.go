package instance

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/proc"
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

// Launch is one run of the game: something stamps it as the game starts and closes it once the game
// has gone. Under a launcher that is the pre-launch and post-exit hooks, which are handed no exit
// code, so the outcome is read from a crash report newer than StartedAt; under `play` it is the
// watcher, which waited for the game and knows the status it left. A run nothing closed keeps an
// open record, with no EndedAt and no Outcome. A run the game never began is closed on the spot as
// OutcomeNotStarted, with StartedAt and EndedAt the same moment, since none of the time that passed
// was the game's.
type Launch struct {
	StartedAt   string `json:"startedAt"`
	EndedAt     string `json:"endedAt,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	Log         string `json:"log,omitempty"`
	CrashReport string `json:"crashReport,omitempty"`
	// PID is the game's own process, and is there only while the record is open. A watcher writes
	// it so that a later command can tell a run still going from one whose watcher was killed; a
	// launcher-driven run has none, and is nobody's to close but its own post-exit hook.
	PID int `json:"pid,omitempty"`
	// Java is the runtime a watched game runs on, kept beside PID and dropped with it: on Windows
	// its jcmd is what takes a thread dump of the game.
	Java string `json:"java,omitempty"`
	// Wrapped is a watched game started through settings.wrapper, whose PID is the wrapper's rather
	// than Java's. It is dropped with PID.
	Wrapped bool `json:"wrapped,omitempty"`
	// ExitCode is the status the game left. Only a run shulker waited on itself has one: no
	// launcher passes the game's exit code to a post-exit slot.
	ExitCode int `json:"exitCode,omitempty"`
	// Error names the executable that would not run and the operating system's reason. It never
	// carries any part of the game's argv, which holds the session access token.
	Error string `json:"error,omitempty"`
}

func LaunchesPath(dir string) string { return filepath.Join(dir, Dir, LaunchesFileName) }

// LaunchLog is where a run's output goes. Every launch gets a file of its own, named for when it
// started, because a detached game has no terminal to write to and the last run's output is what
// says why it stopped. Two launches in the same second are still two runs, so the second takes a
// suffix, the way a history entry does.
func LaunchLog(dir string, at time.Time) (string, error) {
	logs := filepath.Join(dir, Dir, "logs")
	stamp := at.Format("20060102-150405")
	for n := 2; ; n++ {
		path := filepath.Join(logs, stamp+".log")
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", err
		}
		stamp = at.Format("20060102-150405") + "-" + strconv.Itoa(n)
	}
}

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

// UpdateLaunches reads the records, hands them to change and saves what it returns, holding a lock
// throughout: a run's watcher is its own process, and one game ending while another starts would
// otherwise save over the record the other just added.
func UpdateLaunches(dir string, keep int, change func([]Launch) []Launch) error {
	if err := os.MkdirAll(filepath.Join(dir, Dir), 0o755); err != nil {
		return err
	}
	unlock, err := fsutil.Lock(LaunchesPath(dir) + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	return SaveLaunches(dir, change(LoadLaunches(dir)), keep)
}

// NoExitCode stands for a run nothing passed the game's status on from, which is every run a
// launcher drove: there a crash report is the only evidence the outcome can be read from.
const NoExitCode = -1

// NowStamp is the moment a record dates a run from, as StartedAt and EndedAt hold it.
func NowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

// OpenRun stamps the record for a run that is starting. A launcher's pre-launch hook has only the
// moment to write down; a watcher also has the game's own process and the file its output is going
// to, and that pid is what lets a later command close a run whose watcher was killed. At a keep of
// 0 nothing is written.
func OpenRun(dir string, keep int, rec Launch) error {
	if keep == 0 {
		return nil
	}
	return UpdateLaunches(dir, keep, func(records []Launch) []Launch {
		return append(records, rec)
	})
}

// CloseRun ends the open record for the game with this pid, with how the run finished, and hands
// back what it says. A pid of 0 is a launcher's run, which has none, and closes the newest open
// record. exit is NoExitCode where nothing passed the game's status on. At a keep of 0 the run was
// never written down and nothing is written now, but the record still comes back filled in, because
// it is what the command that waited for the game reports.
func CloseRun(dir string, keep, pid, exit int) (Launch, error) {
	rec := Launch{StartedAt: NowStamp()}
	if keep == 0 {
		endRecord(&rec, dir, exit)
		return rec, nil
	}
	found := false
	err := UpdateLaunches(dir, keep, func(records []Launch) []Launch {
		if open := openRecord(records, pid); open >= 0 {
			endRecord(&records[open], dir, exit)
			rec, found = records[open], true
		}
		return records
	})
	if !found {
		endRecord(&rec, dir, exit)
	}
	return rec, err
}

// ReconcileRuns closes the runs whose watchers never did. A record left open with a pid belongs to
// a run shulker was watching itself, so while that process is alive the run is still going; once it
// has gone the record is closed from the crash reports, which is all a watcher that was killed left
// behind. A record with no pid is a launcher's, and nothing but its own post-exit hook closes it. A
// directory with no readable instance file has no runs to reconcile.
func ReconcileRuns(dir string) error {
	f, err := Load(dir)
	if err != nil {
		return nil
	}
	keep := f.Settings.LaunchKeep()
	if keep == 0 || !slices.ContainsFunc(LoadLaunches(dir), isAbandoned) {
		return nil
	}
	return UpdateLaunches(dir, keep, func(records []Launch) []Launch {
		for i := range records {
			if isAbandoned(records[i]) {
				endRecord(&records[i], dir, NoExitCode)
			}
		}
		return records
	})
}

// isAbandoned is an open record for a game shulker was watching that has gone without its watcher
// closing it.
func isAbandoned(rec Launch) bool {
	return rec.EndedAt == "" && rec.PID != 0 && !proc.IsAlive(rec.PID)
}

// openRecord is the newest record nothing has closed for the game with this pid, or -1 where there
// is none. A pid of 0 takes the newest open record of any kind, which is what a launcher's hooks,
// with no process of their own to go by, have always closed.
func openRecord(records []Launch, pid int) int {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].EndedAt == "" && (pid == 0 || records[i].PID == pid) {
			return i
		}
	}
	return -1
}

// endRecord fills in how a run ended. A non-zero status is a crash in its own right; where no status
// was passed on, a crash report newer than the start is the only evidence there is.
func endRecord(rec *Launch, dir string, exit int) {
	started, err := time.Parse(time.RFC3339, rec.StartedAt)
	if err != nil {
		started = time.Time{}
	}
	rec.EndedAt = NowStamp()
	rec.Outcome = OutcomeOK
	rec.PID, rec.Java, rec.Wrapped = 0, "", false
	log, crash := FailureFiles(dir, started)
	if rec.Log == "" {
		rec.Log = log
	}
	if crash != "" {
		rec.Outcome, rec.CrashReport = OutcomeCrashed, crash
	}
	if exit != NoExitCode {
		rec.ExitCode = exit
		if exit != 0 {
			rec.Outcome = OutcomeCrashed
		}
	}
}

// FailLaunch records a run the game never began. It closes the record this run stamped, and opens
// one already closed when there was none, so the history shows the launch either way. An open
// record it did not stamp belongs to an abandoned run and is left alone. Only a keep of 0 silences
// this: the post-exit switch governs a run that ended, and this one never ran. StartedAt and EndedAt
// match, because whatever time passed was shulker's and not the game's.
func FailLaunch(dir string, keep int, stamped bool, reason string) error {
	if keep == 0 {
		return nil
	}
	return UpdateLaunches(dir, keep, func(records []Launch) []Launch {
		at := -1
		if stamped {
			at = openRecord(records, 0)
		}
		if at < 0 {
			records = append(records, Launch{StartedAt: NowStamp()})
			at = len(records) - 1
		}
		records[at].EndedAt = records[at].StartedAt
		records[at].Outcome = OutcomeNotStarted
		records[at].Error = reason
		return records
	})
}

// RunningGame is the open record of a game shulker started and is still running. A launcher's run
// has no pid to signal, so it is no more a running game here than a run that has ended.
func RunningGame(dir string) (Launch, error) {
	records := LoadLaunches(dir)
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		if rec.EndedAt == "" && rec.PID != 0 && proc.IsAlive(rec.PID) {
			return rec, nil
		}
	}
	e := out.Errorf("game-not-running", "no game is running in %s", dir)
	e.Help = "shulker dumps a game it started itself, with shulker play"
	return Launch{}, e
}

// LatestRun is the newest record, which must have a log to read. A launcher's run gets its log only
// once it ends, so while it is open its log is the game's own latest.log.
func LatestRun(dir string) (Launch, error) {
	records := LoadLaunches(dir)
	if len(records) > 0 {
		rec := records[len(records)-1]
		if rec.Log == "" && rec.EndedAt == "" {
			rec.Log = filepath.Join(dir, "logs", "latest.log")
		}
		if rec.Log != "" {
			return rec, nil
		}
	}
	e := out.Errorf("run-not-found", "%s has no run with a log yet", dir)
	e.Help = "start it with shulker play"
	return Launch{}, e
}

// RunClosed reports whether the run being followed has ended: its record was closed, or trimmed
// away by newer runs, or its game has gone without the watcher closing it. A closed record drops its
// pid, so the run is found by when it started.
func RunClosed(dir string, rec Launch) bool {
	records := LoadLaunches(dir)
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].StartedAt == rec.StartedAt {
			return records[i].EndedAt != "" || (rec.PID != 0 && !proc.IsAlive(rec.PID))
		}
	}
	return true
}
