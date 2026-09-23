package cli

import (
	"path/filepath"
	"slices"
	"time"

	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/proc"
)

// noExitCode stands for a run nothing passed the game's status on from, which is every run a
// launcher drove: there a crash report is the only evidence the outcome can be read from.
const noExitCode = -1

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

// openRun stamps the record for a run that is starting. A launcher's pre-launch hook has only the
// moment to write down; a watcher also has the game's own process and the file its output is going
// to, and that pid is what lets a later command close a run whose watcher was killed.
func (a *app) openRun(dir string, s instance.Settings, rec instance.Launch) {
	keep := s.LaunchKeep()
	if keep == 0 {
		return
	}
	err := instance.UpdateLaunches(dir, keep, func(records []instance.Launch) []instance.Launch {
		return append(records, rec)
	})
	if err != nil {
		a.printer.Warn("%v", err)
	}
}

// closeRun ends the open record for the game with this pid, with how the run finished, and hands
// back what it says. A pid of 0 is a launcher's run, which has none, and closes the newest open
// record. exit is noExitCode where nothing passed the game's status on. With launchHistory at 0 the
// run was never written down and nothing is written now, but the record still comes back filled in,
// because it is what the command that waited for the game reports.
func (a *app) closeRun(dir string, s instance.Settings, pid, exit int) instance.Launch {
	keep := s.LaunchKeep()
	rec := instance.Launch{StartedAt: nowStamp()}
	if keep == 0 {
		endRecord(&rec, dir, exit)
		return rec
	}
	found := false
	err := instance.UpdateLaunches(dir, keep, func(records []instance.Launch) []instance.Launch {
		if open := openRecord(records, pid); open >= 0 {
			endRecord(&records[open], dir, exit)
			rec, found = records[open], true
		}
		return records
	})
	if err != nil {
		a.printer.Warn("%v", err)
	}
	if !found {
		endRecord(&rec, dir, exit)
	}
	return rec
}

// reconcileRun closes the runs whose watchers never did. A record left open with a pid belongs to a
// run shulker was watching itself, so while that process is alive the run is still going; once it
// has gone the record is closed from the crash reports, which is all a watcher that was killed left
// behind. A record with no pid is a launcher's, and nothing but its own post-exit hook closes it.
func (a *app) reconcileRun(dir string) {
	f, err := instance.Load(dir)
	if err != nil {
		return
	}
	keep := f.Settings.LaunchKeep()
	if keep == 0 || !slices.ContainsFunc(instance.LoadLaunches(dir), isAbandoned) {
		return
	}
	err = instance.UpdateLaunches(dir, keep, func(records []instance.Launch) []instance.Launch {
		for i := range records {
			if isAbandoned(records[i]) {
				endRecord(&records[i], dir, noExitCode)
			}
		}
		return records
	})
	if err != nil {
		a.printer.Warn("%v", err)
	}
}

// isAbandoned is an open record for a game shulker was watching that has gone without its watcher
// closing it.
func isAbandoned(rec instance.Launch) bool {
	return rec.EndedAt == "" && rec.PID != 0 && !proc.IsAlive(rec.PID)
}

// openRecord is the newest record nothing has closed for the game with this pid, or -1 where there
// is none. A pid of 0 takes the newest open record of any kind, which is what a launcher's hooks,
// with no process of their own to go by, have always closed.
func openRecord(records []instance.Launch, pid int) int {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].EndedAt == "" && (pid == 0 || records[i].PID == pid) {
			return i
		}
	}
	return -1
}

// endRecord fills in how a run ended. A non-zero status is a crash in its own right; where no status
// was passed on, a crash report newer than the start is the only evidence there is.
func endRecord(rec *instance.Launch, dir string, exit int) {
	started, err := time.Parse(time.RFC3339, rec.StartedAt)
	if err != nil {
		started = time.Time{}
	}
	rec.EndedAt = nowStamp()
	rec.Outcome = instance.OutcomeOK
	rec.PID, rec.Java, rec.Wrapped = 0, "", false
	if rec.Log == "" {
		if path := filepath.Join(dir, "logs", "latest.log"); isOnDisk(path) {
			rec.Log = path
		}
	}
	if crash := crashReportSince(dir, started); crash != "" {
		rec.Outcome, rec.CrashReport = instance.OutcomeCrashed, crash
	}
	if exit != noExitCode {
		rec.ExitCode = exit
		if exit != 0 {
			rec.Outcome = instance.OutcomeCrashed
		}
	}
}
