package sync

import (
	"strings"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
)

// InstanceID is the id `-i` takes for a directory, for the message that names it. Empty when the
// registry can't be read, which only costs the message its command.
func InstanceID(e *Env, dir string) string {
	in, _ := e.registered(dir)
	return in.ID
}

// Reconcile brings an instance's hooks in line with its instance.json: a run left open by a
// watcher that was killed is closed first, which is true of every instance, then a launcher with
// a slot gets launcher.Reconcile and a warning for each command it adopted. A launcher shulker
// couldn't set up is worth saying so about, but never worth failing the command that registered it.
// rehooked says a hook that is on was missing or pointed at another binary, and is back.
func Reconcile(e *Env, in config.Instance) (rehooked bool) {
	if err := instance.ReconcileRuns(in.Dir); err != nil {
		e.Warn("%v", err)
	}
	entry := launcher.Find(in.Launcher)
	if !entry.HasHooks() {
		// A plain synced directory has no slot to fill, so it gets no scripts either.
		return false
	}
	var r launcher.Reconciled
	f, err := instance.Load(in.Dir)
	if err == nil {
		var exe string
		if exe, err = launcher.ShulkerPath(); err == nil {
			r, err = launcher.Reconcile(entry, in, f, exe)
		}
	}
	for _, command := range r.Adopted {
		e.warnUnreproducible(*entry.Slot, command)
	}
	if r.CommandsOn {
		e.Warn("turned commands back on in %s for %s, since shulker's hooks run as its commands", entry.Title, in.ID)
	}
	if err != nil {
		e.Warn("hooks not set up for %s: %v", launcher.Named(in), err)
		return false
	}
	return r.Rehooked
}

// warnUnreproducible reports the tokens an adopted command uses that shulker can't reproduce,
// because they come from the launcher's own Java resolution rather than from the instance.
func (e *Env) warnUnreproducible(slot launcher.Slot, command string) {
	var named []string
	for _, token := range slot.Unreproducible {
		if strings.Contains(command, "$"+token) {
			named = append(named, "$"+token)
		}
	}
	if len(named) > 0 {
		e.Warn("the command shulker adopted uses %s, which only the launcher can fill in, so it will be empty when shulker runs it", strings.Join(named, " and "))
	}
}

// refreshRegistered refreshes the row for a directory a sync just built. A sync adds no row of
// its own, so a directory without one is a detached build and gets no hooks either.
func (e *Env) refreshRegistered(in config.Instance) {
	found := false
	e.updateInstances(func(instances []config.Instance) []config.Instance {
		i, ok := config.FindInstance(instances, in.Dir)
		if !ok {
			return instances
		}
		found = true
		in = launcher.RefreshRow(instances[i], in)
		instances[i] = in
		return instances
	})
	if found {
		Reconcile(e, in)
	}
}

// syncLauncherImage keeps a registered instance's picture in its launcher in step with the pack
// icon. An icon it can't use is worth a warning, never a failed sync that would keep the game shut.
func (e *Env) syncLauncherImage(dir string, b *build.Builder) {
	in, ok := e.registered(dir)
	if !ok {
		return
	}
	entry := launcher.Find(in.Launcher)
	if entry == nil || entry.Image == nil {
		return
	}
	icon, err := b.InstanceIcon()
	if err == nil {
		last := instance.LoadState(dir).LauncherImage
		var hash string
		if hash, err = entry.SyncImage(dir, icon, last); err == nil && hash != last {
			err = instance.RecordLauncherImage(dir, hash)
		}
	}
	if err != nil {
		e.Warn("launcher image not updated for %s: %v", launcher.Named(in), err)
	}
}
