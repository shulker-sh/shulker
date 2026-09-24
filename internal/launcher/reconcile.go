package launcher

import (
	"path/filepath"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
)

// Reconcile makes a linked directory match its instance file f: the generated scripts and the
// launcher slots written for the switches that are on, both removed for the switches that are off,
// and settings.shulker pointed at exe, the binary now running. It is idempotent, so link, every
// registering sync and `instances repair` can all call it, and a hand-edited switch takes effect
// through the same path that first set it. A launcher with no slot fills none and gets no scripts.
// adopted is each foreign command the launcher's slots held, moved into f's own settings so the
// generated script keeps running it instead of destroying it.
func Reconcile(e *Entry, in config.Instance, f *instance.File, exe string) (adopted []string, err error) {
	if e == nil || e.Slot == nil {
		return nil, nil
	}
	slot := *e.Slot
	instanceDir := e.InstanceDir(in.Dir)
	current, found, err := ReadSlots(e, in)
	if err != nil || !found {
		return nil, err
	}
	f.Settings.Shulker = exe
	adopted = adoptSlots(f, current)
	if slot.UsesShim {
		captureLauncherJava(f, current.Java)
	}

	var want Slots
	if f.Settings.PreLaunch() {
		h := Hook{
			Dir:      in.Dir,
			Kind:     HookPreLaunch,
			Shulker:  exe,
			Deadline: slot.Deadline,
			Tokens:   instTokenValues(in, instanceDir),
		}
		if f.Settings.Commands != nil {
			h.Adopted = f.Settings.Commands.PreLaunch
		}
		if err := WriteHook(h); err != nil {
			return adopted, err
		}
		want.PreLaunch = slotCommandOf(slot, in, HookPreLaunch)
	} else if err := RemoveHook(in.Dir, HookPreLaunch); err != nil {
		return adopted, err
	}
	if f.Settings.PostExit() {
		h := Hook{
			Dir:     in.Dir,
			Kind:    HookPostExit,
			Shulker: exe,
			Tokens:  instTokenValues(in, instanceDir),
		}
		if f.Settings.Commands != nil {
			h.Adopted = f.Settings.Commands.PostExit
		}
		if err := WriteHook(h); err != nil {
			return adopted, err
		}
		want.PostExit = slotCommandOf(slot, in, HookPostExit)
	} else if err := RemoveHook(in.Dir, HookPostExit); err != nil {
		return adopted, err
	}
	if slot.UsesShim {
		if want.Java, err = reconcileShim(in, f, exe); err != nil {
			return adopted, err
		}
	} else {
		// The shim prepends the wrapper itself; every other launcher has a slot of its own for it.
		want.Wrapper = WrapperCommand(in.Launcher, f.Settings.Wrapper)
	}
	if err := WriteSlots(e, in, want); err != nil {
		return adopted, err
	}
	return adopted, f.Save(in.Dir)
}

// slotCommandOf is what the launcher's own slot holds. A launcher with no command slots keeps them
// empty: its generated scripts are there for the shim to reach, not for the launcher to run.
func slotCommandOf(slot Slot, in config.Instance, kind HookKind) string {
	if slot.UsesShim {
		return ""
	}
	return SlotCommand(in.Launcher, in.Dir, kind)
}

// reconcileShim writes or removes the shim with the hook switches, and reports what the profile's
// Java should be: the shim while a switch is on, else the Java the profile had before shulker.
func reconcileShim(in config.Instance, f *instance.File, exe string) (string, error) {
	if !f.Settings.PreLaunch() && !f.Settings.PostExit() {
		restore := ""
		if f.Resolved != nil {
			restore = f.Resolved.LauncherJava
		}
		return restore, RemoveShim(in.Dir)
	}
	if err := WriteShim(Shim{Dir: in.Dir, Shulker: exe, Java: f.Java()}); err != nil {
		return "", err
	}
	return ShimPath(in.Dir), nil
}

// captureLauncherJava records the Java the profile had before shulker pointed it at the shim, so
// unlink and a switch turned off can put it back. Finding the shim there means this profile is
// already linked, and what was captured the first time stands.
func captureLauncherJava(f *instance.File, current string) {
	if IsShulkerShim(current) {
		return
	}
	if current == "" {
		if f.Resolved != nil {
			f.Resolved.LauncherJava = ""
		}
		return
	}
	f.EnsureResolved().LauncherJava = current
}

// adoptSlots moves a command shulker didn't write into the instance's own settings, so the generated
// script keeps running it instead of destroying it. Extends "sync never deletes a file it didn't
// place" to launcher settings. It returns each command newly adopted.
func adoptSlots(f *instance.File, current Slots) []string {
	var adopted []string
	if c := current.PreLaunch; c != "" && !IsShulkerSlot(c) {
		if f.Settings.Commands == nil {
			f.Settings.Commands = &instance.Commands{}
		}
		if f.Settings.Commands.PreLaunch != c {
			f.Settings.Commands.PreLaunch = c
			adopted = append(adopted, c)
		}
	}
	if c := current.PostExit; c != "" && !IsShulkerSlot(c) {
		if f.Settings.Commands == nil {
			f.Settings.Commands = &instance.Commands{}
		}
		if f.Settings.Commands.PostExit != c {
			f.Settings.Commands.PostExit = c
			adopted = append(adopted, c)
		}
	}
	return adopted
}

// instTokenValues reproduces the launcher's own instance variables, which it stops substituting once
// an adopted command lives inside the generated script. INST_ID is the instance folder's name, which
// is what the launchers substitute, not shulker's own id.
func instTokenValues(in config.Instance, instanceDir string) map[string]string {
	return map[string]string{
		"INST_NAME":   in.Label(),
		"INST_ID":     filepath.Base(instanceDir),
		"INST_DIR":    instanceDir,
		"INST_MC_DIR": in.Dir,
	}
}
