package cli

import (
	"context"
	"path"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/server"
)

// playReport is what `play --dry-run` prints: the launch shulker assembled, with nothing derived
// from an account in it. The argv is deliberately absent, here and everywhere else, because it
// carries the session's access token.
type playReport struct {
	Instance       string `json:"instance"`
	Version        string `json:"version"`
	Inherits       string `json:"inherits,omitempty"`
	MainClass      string `json:"mainClass"`
	Java           string `json:"java"`
	GameDir        string `json:"gameDir"`
	NativesDir     string `json:"nativesDir"`
	AssetIndex     string `json:"assetIndex,omitempty"`
	Classpath      int    `json:"classpath"`
	ClasspathBytes int64  `json:"classpathBytes"`
}

func (a *app) playCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "play [nickname]",
		Short: "Start a shulker instance",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !dryRun {
				return out.Errorf("usage", "shulker play can't start the game yet; pass --dry-run to check the launch it would assemble")
			}
			return a.dryRun(cmd.Context(), args)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "assemble the launch and print it instead of starting the game")
	return cmd
}

func (a *app) dryRun(ctx context.Context, args []string) error {
	in, p, err := a.playInstance(args)
	if err != nil {
		return err
	}
	s, err := a.gameStore()
	if err != nil {
		return err
	}
	versionID, err := a.storeVersion(ctx, p, s)
	if err != nil {
		return err
	}
	top, err := s.Version(versionID)
	if err != nil {
		return err
	}
	v, err := s.Resolve(versionID)
	if err != nil {
		return err
	}
	host := game.Host()
	assembly, err := game.Assemble(v, host, nil)
	if err != nil {
		return err
	}
	if err := a.fillStore(ctx, s, assembly); err != nil {
		return err
	}
	natives := nativesDir(in.Dir)
	if err := assembly.ExtractNatives(s, natives, host); err != nil {
		return err
	}
	java, err := a.clientJava(ctx, p, in.Dir)
	if err != nil {
		return err
	}
	rep := playReport{
		Instance:       in.ID,
		Version:        versionID,
		Inherits:       top.InheritsFrom,
		MainClass:      v.MainClass,
		Java:           java,
		GameDir:        in.Dir,
		NativesDir:     natives,
		Classpath:      len(assembly.Libraries) + 1,
		ClasspathBytes: assembly.ClasspathSize(s),
	}
	if v.AssetIndex != nil {
		rep.AssetIndex = v.AssetIndex.ID
	}
	return a.printer.Emit(rep, func(l *out.Lines) {
		l.OK("would launch "+rep.Instance, rep.Version)
		rows := []out.Row{}
		if rep.Inherits != "" {
			rows = append(rows, out.Row{Label: "inherits", Text: rep.Inherits})
		}
		rows = append(rows,
			out.Row{Label: "main class", Text: rep.MainClass},
			out.Row{Label: "java", Text: rep.Java},
			out.Row{Label: "game dir", Text: rep.GameDir},
			out.Row{Label: "natives", Text: rep.NativesDir},
		)
		if rep.AssetIndex != "" {
			rows = append(rows, out.Row{Label: "asset index", Text: rep.AssetIndex})
		}
		rows = append(rows, out.Row{Label: "classpath", Text: plural(rep.Classpath, "jar", "jars") + ", " + out.HumanBytes(rep.ClasspathBytes)})
		l.Tree(rows...)
	})
}

// playInstance is the instance a launch acts on: the nickname given, else the one the current
// directory is. Only the instances shulker owns can be launched from here; every other launcher
// starts its own.
func (a *app) playInstance(args []string) (config.Instance, *project.Project, error) {
	if len(args) == 1 {
		if a.instance != "" || a.dir != "" {
			return config.Instance{}, nil, out.Errorf("usage", "pass a nickname, -i or -C, not more than one: each of them says which instance to launch")
		}
		a.instance = args[0]
	}
	dir, err := a.scopeDir()
	if err != nil {
		return config.Instance{}, nil, err
	}
	in, ok := a.registeredInstance(dir)
	if !ok {
		return config.Instance{}, nil, out.Errorf("instance-not-found", "%s is not a registered instance; `shulker link shulker` makes one shulker launches itself", dir)
	}
	if in.Launcher != "shulker" {
		return config.Instance{}, nil, out.Errorf("not-shulker", "%s belongs to %s, which starts it itself; shulker only launches the instances it owns", in.ID, in.Launcher)
	}
	p, err := a.openProjectAt(dir)
	if err != nil {
		return config.Instance{}, nil, err
	}
	if err := a.requireLock(p); err != nil {
		return config.Instance{}, nil, err
	}
	return in, p, nil
}

func (a *app) gameStore() (game.Store, error) {
	r, err := a.roots()
	if err != nil {
		return game.Store{}, err
	}
	d, err := a.deps()
	if err != nil {
		return game.Store{}, err
	}
	s := game.Store{Root: r.Store, Resources: d.resources}
	return s, s.EnsureProfiles()
}

// nativesDir is where a launch unpacks its native libraries. It sits under .shulker/ rather than
// in the game directory, which belongs to the pack.
func nativesDir(dir string) string { return filepath.Join(dir, instance.Dir, "natives") }

// storeVersion puts the version JSON a launch runs, and the vanilla one it inherits from, into the
// store, and returns its id. A loader with an installer of its own is run against the store, which
// is laid out as the Mojang launcher directory the installer expects.
func (a *app) storeVersion(ctx context.Context, p *project.Project, s game.Store) (string, error) {
	d, err := a.deps()
	if err != nil {
		return "", err
	}
	if !s.HasVersion(p.Lock.Minecraft) {
		a.progress("fetching the minecraft %s version json", p.Lock.Minecraft)
		raw, err := d.meta.Piston.Version(ctx, p.Lock.Minecraft)
		if err != nil {
			return "", err
		}
		if _, err := s.SaveVersion(raw); err != nil {
			return "", err
		}
	}
	if p.Lock.Loader.Type == "" {
		return p.Lock.Minecraft, nil
	}
	l, err := loader.Require(p.Lock.Loader.Type)
	if err != nil {
		return "", err
	}
	if l.InstallClientFlag != "" {
		return a.installedLoader(ctx, p, s, l)
	}
	a.progress("fetching %s loader %s for %s", p.Lock.Loader.Type, p.Lock.Loader.Version, p.Lock.Minecraft)
	profile, err := d.meta.LoaderProfile(ctx, p.Lock.Loader, p.Lock.Minecraft)
	if err != nil {
		return "", err
	}
	return s.SaveVersion(profile)
}

// fillStore fetches everything the assembly is missing, a bar per kind so each stage of the
// assembly says what it did. Assets come last, because their index has to be in the store before
// the objects it names can be listed.
func (a *app) fillStore(ctx context.Context, s game.Store, assembly game.Assembly) error {
	if err := a.fetchInto(ctx, s, []game.File{assembly.Client}, "client jar", "client jars"); err != nil {
		return err
	}
	libraries := append(append([]game.File{}, assembly.Libraries...), assembly.Natives...)
	if err := a.fetchInto(ctx, s, libraries, "library", "libraries"); err != nil {
		return err
	}
	if assembly.AssetIndex.Path == "" {
		return nil
	}
	if err := a.fetchInto(ctx, s, []game.File{assembly.AssetIndex}, "asset index", "asset indexes"); err != nil {
		return err
	}
	objects, err := s.AssetFiles(assembly.Version.AssetIndex.ID)
	if err != nil {
		return err
	}
	return a.fetchInto(ctx, s, objects, "asset", "assets")
}

// storeDownloadJobs is how many files the store fetches at once. An asset index names thousands of
// small objects, and fetching them one at a time is what makes a first launch take hours.
const storeDownloadJobs = 8

func (a *app) fetchInto(ctx context.Context, s game.Store, files []game.File, one, many string) error {
	d, err := a.deps()
	if err != nil {
		return err
	}
	var missing []game.File
	for _, f := range files {
		if !s.Has(f) {
			missing = append(missing, f)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	downloads := make([]out.Download, len(missing))
	for i, f := range missing {
		downloads[i] = out.Download{Name: path.Base(f.Path), Size: f.Size}
	}
	progress := a.printer.Progress("fetching", downloads).Counts(one, many)
	if progress != nil {
		d.fetch.Progress = progress.Bytes
		defer func() { d.fetch.Progress = nil }()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
		slots    = make(chan struct{}, storeDownloadJobs)
	)
	for i, f := range missing {
		if ctx.Err() != nil {
			break
		}
		slots <- struct{}{}
		wg.Add(1)
		go func(name string, f game.File) {
			defer wg.Done()
			defer func() { <-slots }()
			progress.File(name)
			if err := s.Fetch(ctx, d.fetch, f); err != nil {
				once.Do(func() { firstErr = err; cancel() })
				return
			}
			progress.Advance()
		}(downloads[i].Name, f)
	}
	wg.Wait()
	if firstErr != nil {
		progress.Abort()
		return firstErr
	}
	progress.Finish()
	return nil
}

// clientJava is what the launch runs: the instance's own `java` setting when it has one, and
// otherwise shulker's managed runtime for the component the lock names.
func (a *app) clientJava(ctx context.Context, p *project.Project, dir string) (string, error) {
	f, err := instance.Load(dir)
	if err != nil {
		return "", err
	}
	if f.Settings.Java != "" {
		java, err := server.FindJava(f.Settings.Java, p.Lock.Java.Major)
		if err != nil {
			return "", err
		}
		return java.Path, nil
	}
	rt, err := a.freshestJava(ctx, p, linkJavaFix("shulker"))
	if err != nil {
		return "", err
	}
	return server.JavaBin(rt.Home), nil
}

// installedLoader runs a loader's own installer against the store and remembers the version id it
// wrote, so a second launch of the same pack doesn't pay for the installer again.
func (a *app) installedLoader(ctx context.Context, p *project.Project, s game.Store, l loader.Loader) (string, error) {
	key := p.Lock.Loader.Type + "-" + p.Lock.Loader.Version + "-" + p.Lock.Minecraft
	if id, ok := s.InstalledLoader(key); ok && s.HasVersion(id) {
		return id, nil
	}
	id, err := a.installClientLoader(ctx, p, &launcher.Mojang{Dir: s.Root}, l)
	if err != nil {
		return "", err
	}
	return id, s.RecordLoader(key, id)
}
