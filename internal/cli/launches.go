package cli

import (
	"path/filepath"
	"time"

	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
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
	if err := instance.SaveLaunches(dir, append(instance.LoadLaunches(dir), rec), keep); err != nil {
		a.printer.Warn("%v", err)
	}
}

// closeRun ends the newest open record with how the run finished, and hands back what it says. exit
// is noExitCode where nothing passed the game's status on. With launchHistory at 0 the run was never
// written down and nothing is written now, but the record still comes back filled in, because it is
// what the command that waited for the game reports.
func (a *app) closeRun(dir string, s instance.Settings, exit int) instance.Launch {
	keep := s.LaunchKeep()
	records := instance.LoadLaunches(dir)
	open := openRecord(records)
	if keep == 0 || open < 0 {
		rec := instance.Launch{StartedAt: nowStamp()}
		endRecord(&rec, dir, exit)
		return rec
	}
	endRecord(&records[open], dir, exit)
	if err := instance.SaveLaunches(dir, records, keep); err != nil {
		a.printer.Warn("%v", err)
	}
	return records[open]
}

// reconcileRun closes a run whose watcher never did. A record left open with a pid belongs to a run
// shulker was watching itself, so while that process is alive the run is still going; once it has
// gone the record is closed from the crash reports, which is all a watcher that was killed left
// behind. A record with no pid is a launcher's, and nothing but its own post-exit hook closes it.
func (a *app) reconcileRun(dir string) {
	f, err := instance.Load(dir)
	if err != nil {
		return
	}
	records := instance.LoadLaunches(dir)
	open := openRecord(records)
	if open < 0 || records[open].PID == 0 || game.Alive(records[open].PID) {
		return
	}
	a.closeRun(dir, f.Settings, noExitCode)
}

// openRecord is the newest record nothing has closed, or -1 where every run is accounted for.
func openRecord(records []instance.Launch) int {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].EndedAt == "" {
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
	rec.PID = 0
	if rec.Log == "" {
		if path := filepath.Join(dir, "logs", "latest.log"); fileExists(path) {
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
