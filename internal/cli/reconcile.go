package cli

import (
	"path/filepath"
	"strings"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
)

// reconcileInstance makes a linked directory match its instance.json: the generated scripts and the
// launcher slots for the switches that are on, both removed for the switches that are off, and
// settings.shulker pointed at the binary now running. It is idempotent, so link, every registering
// sync and `instances repair` can all call it, and a hand-edited switch takes effect through the
// same path that first set it.
func (a *app) reconcileInstance(in config.Instance) error {
	e := launcher.Find(in.Launcher)
	slot, fills := launcher.SlotOf(in.Launcher)
	if e == nil || !fills {
		// A plain synced directory has no slot to fill, so it gets no scripts either.
		return nil
	}
	f, err := instance.Load(in.Dir)
	if err != nil {
		return err
	}
	instanceDir := e.InstanceDir(in.Dir)
	current, found, err := launcher.ReadSlots(e, in)
	if err != nil || !found {
		return err
	}
	exe, err := shulkerPath()
	if err != nil {
		return err
	}
	f.Settings.Shulker = exe
	a.adoptSlots(f, slot, current)
	if slot.Shim {
		captureLauncherJava(f, current.Java)
	}

	var want launcher.Slots
	if f.Settings.PreLaunch() {
		h := launcher.Hook{
			Dir:      in.Dir,
			Kind:     launcher.HookPreLaunch,
			Shulker:  exe,
			Deadline: slot.Deadline,
			Tokens:   instTokenValues(in, instanceDir),
		}
		if f.Settings.Commands != nil {
			h.Adopted = f.Settings.Commands.PreLaunch
		}
		if err := launcher.WriteHook(h); err != nil {
			return err
		}
		want.PreLaunch = slotCommandOf(slot, in, launcher.HookPreLaunch)
	} else if err := launcher.RemoveHook(in.Dir, launcher.HookPreLaunch); err != nil {
		return err
	}
	if f.Settings.PostExit() {
		h := launcher.Hook{
			Dir:     in.Dir,
			Kind:    launcher.HookPostExit,
			Shulker: exe,
			Tokens:  instTokenValues(in, instanceDir),
		}
		if f.Settings.Commands != nil {
			h.Adopted = f.Settings.Commands.PostExit
		}
		if err := launcher.WriteHook(h); err != nil {
			return err
		}
		want.PostExit = slotCommandOf(slot, in, launcher.HookPostExit)
	} else if err := launcher.RemoveHook(in.Dir, launcher.HookPostExit); err != nil {
		return err
	}
	if slot.Shim {
		if want.Java, err = reconcileShim(in, f, current.Java, exe); err != nil {
			return err
		}
	}
	if err := launcher.WriteSlots(e, in, want); err != nil {
		return err
	}
	return f.Save(in.Dir)
}

// slotCommandOf is what the launcher's own slot holds. A launcher with no command slots keeps them
// empty: its generated scripts are there for the shim to reach, not for the launcher to run.
func slotCommandOf(slot launcher.Slot, in config.Instance, kind launcher.HookKind) string {
	if slot.Shim {
		return ""
	}
	return launcher.SlotCommand(in.Launcher, in.Dir, kind)
}

// reconcileShim writes or removes the shim with the hook switches, and reports what the profile's
// Java should be: the shim while a switch is on, else the Java the profile had before shulker.
func reconcileShim(in config.Instance, f *instance.File, current, exe string) (string, error) {
	if !launcher.ShimSupported() {
		return current, nil
	}
	if !f.Settings.PreLaunch() && !f.Settings.PostExit() {
		restore := ""
		if f.Resolved != nil {
			restore = f.Resolved.LauncherJava
		}
		return restore, launcher.RemoveShim(in.Dir)
	}
	java := f.Settings.Java
	if java == "" && f.Resolved != nil {
		java = f.Resolved.Java
	}
	if err := launcher.WriteShim(launcher.Shim{Dir: in.Dir, Shulker: exe, Java: java}); err != nil {
		return "", err
	}
	return launcher.ShimPath(in.Dir), nil
}

// captureLauncherJava records the Java the profile had before shulker pointed it at the shim, so
// unlink and a switch turned off can put it back. Finding the shim there means this profile is
// already linked, and what was captured the first time stands.
func captureLauncherJava(f *instance.File, current string) {
	if launcher.IsShulkerShim(current) {
		return
	}
	if current == "" {
		if f.Resolved != nil {
			f.Resolved.LauncherJava = ""
		}
		return
	}
	if f.Resolved == nil {
		f.Resolved = &instance.Resolved{}
	}
	f.Resolved.LauncherJava = current
}

// adoptSlots moves a command shulker didn't write into the instance's own settings, so the generated
// script keeps running it instead of destroying it. Extends "sync never deletes a file it didn't
// place" to launcher settings.
func (a *app) adoptSlots(f *instance.File, slot launcher.Slot, current launcher.Slots) {
	if c := current.PreLaunch; c != "" && !launcher.IsShulkerSlot(c) {
		if f.Settings.Commands == nil {
			f.Settings.Commands = &instance.Commands{}
		}
		if f.Settings.Commands.PreLaunch != c {
			f.Settings.Commands.PreLaunch = c
			a.warnUnreproducible(slot, c)
		}
	}
	if c := current.PostExit; c != "" && !launcher.IsShulkerSlot(c) {
		if f.Settings.Commands == nil {
			f.Settings.Commands = &instance.Commands{}
		}
		if f.Settings.Commands.PostExit != c {
			f.Settings.Commands.PostExit = c
			a.warnUnreproducible(slot, c)
		}
	}
}

// warnUnreproducible reports the tokens an adopted command uses that shulker can't reproduce,
// because they come from the launcher's own Java resolution rather than from the instance.
func (a *app) warnUnreproducible(slot launcher.Slot, command string) {
	var named []string
	for _, token := range slot.Unreproducible {
		if strings.Contains(command, "$"+token) {
			named = append(named, "$"+token)
		}
	}
	if len(named) > 0 {
		a.printer.Warn("the command shulker adopted uses %s, which only the launcher can fill in, so it will be empty when shulker runs it", strings.Join(named, " and "))
	}
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
