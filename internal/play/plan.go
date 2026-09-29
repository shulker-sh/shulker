package play

import (
	"context"
	"time"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/java"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/resolve"
	"shulker.sh/shulker/internal/security"
	"shulker.sh/shulker/internal/sync"
)

// Request is how one launch differs from the next before anyone is playing it: whether the
// instance is brought up to date first, what the history entry names as the reason, and where the
// game boots to.
type Request struct {
	Sync bool
	// Reason names the command for the sync's history entry.
	Reason string
	Target game.QuickPlay
}

// Plan is everything a launch needs that doesn't depend on who is playing: the version it runs,
// the store filled with what that version names, the natives unpacked, the Java to run it with and
// the settings it runs under. A dry run prints it; a launch templates the account into it.
type Plan struct {
	Instance   config.Instance
	Project    *project.Project
	Launchable game.Launchable
	Natives    string
	Java       string
	Platform   game.Platform
	Settings   instance.Settings
	Target     game.QuickPlay
	// Sync is the sync that ran first, nil when none did.
	Sync *sync.Result
}

// Assemble is the plan for launching in: the sync when the request and the instance's hooks ask
// for one, then the project as that sync left it, the store filled for its lock, and the settings
// it runs with. A run whose watcher was killed is still open in the record, and this is the next
// thing touching the instance, so it closes it first.
func Assemble(ctx context.Context, e *Env, in config.Instance, req Request) (*Plan, error) {
	if err := instance.ReconcileRuns(in.Dir); err != nil {
		e.Warn("%v", err)
	}
	f, err := instance.Load(in.Dir)
	if err != nil {
		return nil, err
	}
	plan := &Plan{Instance: in, Target: req.Target}
	if req.Sync && f.Settings.PreLaunch() {
		res, err := sync.ForLaunch(ctx, e.Env, in.Dir, req.Reason)
		refusal, refused := security.Refused(err)
		switch {
		case refused && hasBuild(in.Dir):
			e.WarnNudge(refusal.Nudge, "%s", security.Warning(refusal))
		case err != nil:
			return nil, err
		default:
			plan.Sync = &res
			// The sync may have rewritten the file, and the project is opened only now, since a
			// sync is what brings the lock the launch is assembled from up to date.
			if f, err = instance.Load(in.Dir); err != nil {
				return nil, err
			}
		}
	}
	p, err := project.Open(in.Dir)
	if err != nil {
		return nil, err
	}
	if err := sync.RequireLock(e.Env, p); err != nil {
		return nil, err
	}
	plan.Project = p
	// A pack that can't be read costs the launch only its client.memory, never the launch: the
	// sync before it has already said what is wrong, and the game starts from what is on disk.
	if _, err := resolve.Packs(ctx, e.Env.Env, p); err != nil {
		e.Warn("using %s: the modpacks couldn't be read for a client.memory: %v.", instance.DefaultMemory, err)
	}
	if plan.Settings, err = settings(e, f, p); err != nil {
		return nil, err
	}
	if plan.Java, err = clientJava(ctx, e, p, plan.Settings); err != nil {
		return nil, err
	}
	// The platform decides which natives are fetched, so the Java is chosen before the fill.
	plan.Platform = game.ForJava(plan.Java)
	src, err := sync.GameSources(e.Env, p)
	if err != nil {
		return nil, err
	}
	if err := e.Store.EnsureProfiles(); err != nil {
		return nil, err
	}
	if plan.Launchable, err = e.Store.Fill(ctx, p.Lock, plan.Platform, src); err != nil {
		return nil, sync.KeepInstallerOutput(e.Env, err)
	}
	plan.Natives = instance.NativesDir(in.Dir)
	if err := plan.Launchable.Assembly.ExtractNatives(e.Store, plan.Natives, plan.Platform); err != nil {
		return nil, err
	}
	if err := req.Target.Check(plan.Launchable.Version, p.Lock.Minecraft); err != nil {
		return nil, err
	}
	return plan, nil
}

// settings are what a launch runs with: the instance's own where it sets one, the play.* default
// in config.json where it doesn't, and for memory the pack author's client.memory and then the
// fixed default after those. A list the instance sets, even to nothing, replaces the default
// rather than adding to it.
func settings(e *Env, f *instance.File, p *project.Project) (instance.Settings, error) {
	cfg, err := config.LoadFile(e.Config)
	if err != nil {
		return instance.Settings{}, err
	}
	s := f.Settings
	s.LaunchSettings = s.LaunchSettings.ForLaunch(cfg.Play.LaunchSettings, p.ClientMemory())
	return s, nil
}

// clientJava is what the launch runs: the java setting when the instance or play.java has one, and
// otherwise shulker's managed runtime for the component the lock names.
func clientJava(ctx context.Context, e *Env, p *project.Project, s instance.Settings) (string, error) {
	if s.Java != "" {
		bin, err := java.Client(s.Java, p.Lock.Java.Major)
		if err != nil {
			return "", err
		}
		return bin.Path, nil
	}
	rt, err := sync.FreshestJava(ctx, e.Env, p, sync.LinkJavaFix("shulker"))
	if err != nil {
		return "", err
	}
	return java.Bin(rt.Home), nil
}

// Launch is the plan with an account templated in: the launch the watcher starts, or a foreground
// run does. window overrides the settings' for this run. Its argv carries the session token, so it
// goes to Java and nowhere else.
func (p *Plan) Launch(e *Env, session account.Account, window string, now time.Time) (game.Launch, error) {
	vars := p.Launchable.Assembly.Vars(e.Store, "shulker", e.Version, p.Instance.Dir, p.Natives)
	for name, value := range game.SessionOf(session).Vars() {
		vars[name] = value
	}
	log, err := instance.LaunchLog(p.Instance.Dir, now)
	if err != nil {
		return game.Launch{}, err
	}
	if window == "" {
		window = p.Settings.Window
	}
	return game.Launch{
		Java:    p.Java,
		Argv:    game.LaunchArgv(p.Launchable.Version, p.Platform, vars, p.Settings.Memory, p.Settings.JVMArgs, window, p.Target),
		Dir:     p.Instance.Dir,
		Log:     log,
		Wrapper: p.Settings.Wrapper,
	}, nil
}

// hasBuild reports whether dir holds a build to launch when a protection refused its sync.
func hasBuild(dir string) bool {
	st, err := instance.ReadState(dir)
	return err == nil && len(st.Files) > 0
}
