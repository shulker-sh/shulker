package sync

import (
	"errors"
	"io/fs"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/schema"
)

// LoadLocal reads dir's shulker.local.json, warning and going on with an empty one when it had to
// move an unreadable file aside.
func LoadLocal(e *Env, dir string) (*local.File, error) {
	lf, err := local.Load(dir)
	var unreadable *local.UnreadableError
	if errors.As(err, &unreadable) {
		if unreadable.Newer() {
			e.WarnNudge(schema.UpdateNudge, "%v", err)
		} else {
			e.Warn("%v", err)
		}
		return lf, nil
	}
	return lf, err
}

// SaveLocal writes lf with the detected OS, adding it to a project's .gitignore the first time.
func SaveLocal(e *Env, lf *local.File, inProject bool) error {
	created := !lf.Exists()
	lf.DetectedOS = build.DetectOS()
	if err := lf.Save(); err != nil {
		return err
	}
	if !created || !inProject {
		return nil
	}
	added, err := local.AddToGitignore(lf.Dir())
	if added {
		e.Log("added /%s to .gitignore", local.FileName)
	}
	return err
}

// RefreshLocal saves the bookkeeping a build or sync collected. It is best effort: a project
// nobody can write to (someone else's, synced from) keeps its directories in the links registry
// instead, so there is nothing to report.
func RefreshLocal(e *Env, lf *local.File, inProject, changed bool) {
	if !changed && (!lf.Exists() || lf.DetectedOS == build.DetectOS()) {
		return
	}
	if err := SaveLocal(e, lf, inProject); err != nil && !errors.Is(err, fs.ErrPermission) {
		e.Warn("%s not updated: %v", local.FileName, err)
	}
}
