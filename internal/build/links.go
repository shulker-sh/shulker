package build

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

const DataDir = "data"

var clientDataDirs = []string{"saves", "screenshots", "logs", "crash-reports"}

func dataDirs(side, levelName string) []string {
	if side == "server" {
		return []string{levelName, "logs", "crash-reports"}
	}
	return append([]string(nil), clientDataDirs...)
}

type linkPlan struct {
	link   []string
	move   []string
	remove []string
}

func (b *Builder) planLinks(dir, target string, dirs []string, prev State, report *Report) (linkPlan, error) {
	var plan linkPlan
	dataRoot := filepath.Join(b.Dir, DataDir, target)
	wanted := map[string]bool{}
	for _, rel := range dirs {
		wanted[rel] = true
		abs := filepath.Join(dir, rel)
		data := filepath.Join(dataRoot, rel)
		want, err := filepath.Rel(dir, data)
		if err != nil {
			return plan, err
		}
		info, err := os.Lstat(abs)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			plan.link = append(plan.link, rel)
		case err != nil:
			return plan, err
		case info.Mode()&fs.ModeSymlink != 0:
			if current, err := os.Readlink(abs); err != nil || current != want {
				plan.link = append(plan.link, rel)
			}
		case !info.IsDir():
			report.Conflicts = append(report.Conflicts, rel+" (a file is in the way of the data link)")
		default:
			empty, err := isEmptyDir(abs)
			if err != nil {
				return plan, err
			}
			if _, err := os.Lstat(data); err == nil && !empty {
				report.Conflicts = append(report.Conflicts, fmt.Sprintf("%s (exists in both %s and %s; merge by hand)", rel, dir, dataRoot))
				continue
			} else if err == nil || empty {
				plan.link = append(plan.link, rel)
			} else {
				plan.move = append(plan.move, rel)
				plan.link = append(plan.link, rel)
			}
		}
	}
	for _, rel := range prev.Links {
		if wanted[rel] {
			continue
		}
		if info, err := os.Lstat(filepath.Join(dir, rel)); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			plan.remove = append(plan.remove, rel)
		}
	}
	sort.Strings(plan.link)
	return plan, nil
}

func (b *Builder) applyLinks(dir, target string, plan linkPlan, report *Report) error {
	dataRoot := filepath.Join(b.Dir, DataDir, target)
	if len(plan.move)+len(plan.link) > 0 {
		if err := os.MkdirAll(dataRoot, 0o755); err != nil {
			return err
		}
	}
	for _, rel := range plan.move {
		if err := os.Rename(filepath.Join(dir, rel), filepath.Join(dataRoot, rel)); err != nil {
			return err
		}
		report.Moved = append(report.Moved, rel)
	}
	for _, rel := range plan.remove {
		abs := filepath.Join(dir, rel)
		data, err := os.Readlink(abs)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(data) {
			data = filepath.Join(filepath.Dir(abs), data)
		}
		if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		report.Removed = append(report.Removed, rel)
		if empty, err := isEmptyDir(data); err != nil || empty {
			continue
		}
		if err := os.Rename(data, abs); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s stays in %s; move it into %s by hand (%v)", rel, data, abs, err))
			continue
		}
		report.MovedBack = append(report.MovedBack, rel)
	}
	for _, rel := range plan.link {
		abs := filepath.Join(dir, rel)
		data := filepath.Join(dataRoot, rel)
		if err := os.MkdirAll(data, 0o755); err != nil {
			return err
		}
		if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		want, err := filepath.Rel(dir, data)
		if err != nil {
			return err
		}
		if err := os.Symlink(want, abs); err != nil {
			return err
		}
		report.Linked = append(report.Linked, rel)
	}
	return nil
}

func isEmptyDir(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
