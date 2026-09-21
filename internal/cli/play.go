package cli

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/account"
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
	// LoaderLibraries counts the jars the version brings on top of the one it inherits from.
	LoaderLibraries      int   `json:"loaderLibraries,omitempty"`
	LoaderLibrariesBytes int64 `json:"loaderLibrariesBytes,omitempty"`
}

// playResult is what a launch reports once the game is running: which instance started, who is
// playing, and where its output is going. The argv is absent here for the same reason it is absent
// from the dry run.
type playResult struct {
	Instance string     `json:"instance"`
	Version  string     `json:"version"`
	Account  accountRow `json:"account"`
	PID      int        `json:"pid"`
	GameDir  string     `json:"gameDir"`
	Log      string     `json:"log"`
	// Outcome, ExitCode and CrashReport are how the run ended, and are there only when shulker
	// waited for the game itself. A detached launch returns while the game is still running, so
	// what it has to report is that the game started, and the record is the watcher's to close.
	Outcome     string      `json:"outcome,omitempty"`
	ExitCode    int         `json:"exitCode,omitempty"`
	CrashReport string      `json:"crashReport,omitempty"`
	Sync        *syncResult `json:"sync,omitempty"`
}

func (a *app) playCmd() *cobra.Command {
	var opts playOptions
	cmd := &cobra.Command{
		Use:   "play [nickname]",
		Short: "Start a shulker instance",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.window != "" {
				if err := checkPlaySetting("--window", "window", opts.window); err != nil {
					return err
				}
			}
			target, err := parseQuickPlay(cmd, opts.world, opts.server)
			if err != nil {
				return err
			}
			opts.target = target
			if opts.dryRun {
				return a.dryRun(cmd.Context(), args, target)
			}
			return a.play(cmd, args, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "assemble the launch and print it instead of starting the game")
	cmd.Flags().BoolVar(&opts.noSync, "no-sync", false, "start the game without updating the instance first")
	cmd.Flags().BoolVar(&opts.wait, "wait", false, "wait for the game and record how the run ended before returning")
	cmd.Flags().BoolVar(&opts.stream, "stream", false, "wait for the game and mirror its output to the terminal")
	cmd.Flags().StringVar(&opts.account, "account", "", "play as this account (default: the instance's pinned account, else the default account)")
	cmd.Flags().StringVar(&opts.window, "window", "", "open the game at this size for this run, like 1280x720")
	cmd.Flags().StringVar(&opts.world, "world", "", "boot straight into this save, named by its folder in saves/")
	cmd.Flags().StringVar(&opts.server, "server", "", "join this server straight away, as <address>[:<port>]")
	return cmd
}

// playOptions is how one launch differs from the next: which account plays it, whether the instance
// is brought up to date first, and who waits for the game — the watcher, or this command.
type playOptions struct {
	dryRun  bool
	noSync  bool
	wait    bool
	stream  bool
	account string
	window  string
	world   string
	server  string
	target  quickPlay
}

// waits reports whether this command stays for the run. --stream is --wait that shows its working,
// so either of them keeps the launch in the foreground.
func (o playOptions) waits() bool { return o.wait || o.stream }

func (a *app) play(cmd *cobra.Command, args []string, opts playOptions) error {
	ctx := cmd.Context()
	in, err := a.playInstance(args)
	if err != nil {
		return err
	}
	// A run whose watcher was killed is still open in the record; this is the next command touching
	// the instance, so it is the one that closes it.
	a.reconcileRun(in.Dir)
	f, err := instance.Load(in.Dir)
	if err != nil {
		return err
	}
	var synced *syncResult
	if !opts.noSync && f.Settings.PreLaunch() {
		res, err := a.syncForLaunch(cmd, in.Dir)
		if err != nil {
			return err
		}
		synced = &res
	}
	// The project is opened after the sync, since a sync is what brings the lock the launch is
	// assembled from up to date.
	p, err := a.playProject(in.Dir)
	if err != nil {
		return err
	}
	if err := a.checkQuickPlay(ctx, p, opts.target); err != nil {
		return err
	}
	settings, err := a.launchSettings(in.Dir)
	if err != nil {
		return err
	}
	selector := opts.account
	if selector == "" && settings.Account != "" {
		if selector, err = a.pinnedAccount(settings.Account); err != nil {
			return err
		}
	}
	who, adopted, err := a.launchAccount(selector)
	if err != nil {
		return err
	}
	signed, err := a.accountSession(ctx, who)
	if err != nil {
		return err
	}
	// Read back after resolving, so the row says the account is the default one when resolving it
	// is what made it so.
	_, cfg, err := a.accounts()
	if err != nil {
		return err
	}
	plan, err := a.assemble(ctx, in, p)
	if err != nil {
		return err
	}
	vars := plan.assembly.Vars(plan.store, "shulker", version, in.Dir, plan.natives)
	for name, value := range gameSession(signed).Vars() {
		vars[name] = value
	}
	log, err := launchLog(in.Dir, time.Now())
	if err != nil {
		return err
	}
	res := playResult{
		Instance: in.ID,
		Version:  plan.versionID,
		Account:  rowFor(who, cfg),
		GameDir:  in.Dir,
		Log:      log,
		Sync:     synced,
	}
	window := settings.Window
	if opts.window != "" {
		window = opts.window
	}
	req := watchRequest{
		Dir:     in.Dir,
		Java:    plan.java,
		Argv:    launchArgv(plan.version, vars, settings, window, opts.target),
		Log:     res.Log,
		Wrapper: settings.Wrapper,
	}
	a.progress("starting %s as %s", in.ID, who.Name)
	if opts.waits() {
		var rec instance.Launch
		if res.PID, rec, err = a.playWaited(req, opts.stream); err != nil {
			return err
		}
		res.Outcome, res.ExitCode, res.CrashReport = rec.Outcome, rec.ExitCode, rec.CrashReport
	} else if res.PID, err = a.startWatcher(req); err != nil {
		return err
	}
	return a.printer.Emit(res, func(l *out.Lines) {
		if synced != nil {
			synced.print(l)
		}
		if adopted {
			l.Info(who.Name + " is the default account now")
		}
		res.print(l)
	})
}

// playWaited keeps the launch in the foreground: this command starts the game, waits for it, and
// closes the record itself, so no watcher is spawned and nothing is left running behind it. It hands
// back the game's pid and the record of how the run ended.
func (a *app) playWaited(req watchRequest, stream bool) (int, instance.Launch, error) {
	var mirror io.Writer
	if stream {
		mirror = a.printer.Stdout
		if a.printer.JSON {
			mirror = a.printer.Stderr
		}
	}
	a.printer.Settle()
	var pid int
	rec := a.watchRun(req, mirror, func(r watchReply) { pid = r.PID })
	if rec.Outcome == instance.OutcomeNotStarted {
		return 0, rec, notStarted(rec.Error)
	}
	return pid, rec, nil
}

// print is the launch as a player reads it. A detached launch says the game is playing, because that
// is all it knows; a run this command waited for says how it went instead, and a crash is reported
// rather than raised — the game ran, so shulker did its job.
func (r playResult) print(l *out.Lines) {
	rows := []out.Row{
		{Label: "account", Text: r.Account.Name},
		{Label: "log", Text: r.Log},
	}
	switch r.Outcome {
	case "":
		l.OK("playing "+r.Instance, r.Version)
	case instance.OutcomeCrashed:
		l.Warn(r.Instance + " crashed")
		if r.ExitCode != 0 {
			rows = append(rows, out.Row{Label: "status", Text: strconv.Itoa(r.ExitCode)})
		}
		if r.CrashReport != "" {
			rows = append(rows, out.Row{Label: "crash report", Text: r.CrashReport})
		}
	default:
		l.OK("played "+r.Instance, r.Version)
	}
	l.Tree(rows...)
}

// launchSettings are the settings a launch runs with: the instance's own where it sets one, and the
// play.* default in config.json where it doesn't. A list the instance sets, even to nothing,
// replaces the default rather than adding to it.
func (a *app) launchSettings(dir string) (instance.Settings, error) {
	f, err := instance.Load(dir)
	if err != nil {
		return instance.Settings{}, err
	}
	path, err := a.configFile()
	if err != nil {
		return instance.Settings{}, err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return instance.Settings{}, err
	}
	s := f.Settings
	if s.Memory == "" {
		s.Memory = cfg.Play.Memory
	}
	if s.JvmArgs == nil {
		s.JvmArgs = cfg.Play.JvmArgs
	}
	if s.Java == "" {
		s.Java = cfg.Play.Java
	}
	if s.Window == "" {
		s.Window = cfg.Play.Window
	}
	if s.Wrapper == nil {
		s.Wrapper = cfg.Play.Wrapper
	}
	return s, nil
}

// pinnedAccount is the selector for the account an instance is pinned to. A pin whose account has
// gone fails the launch rather than playing as someone else: the pin is there because this
// instance is meant to be played as that account.
func (a *app) pinnedAccount(id string) (string, error) {
	accounts, _, err := a.accounts()
	if err != nil {
		return "", err
	}
	for _, r := range accounts {
		if r.ID == id {
			return id, nil
		}
	}
	e := out.Errorf("account-not-found", "this instance is pinned to account %s, which shulker can no longer see", id)
	e.Candidates, e.Pass = accountCandidates(accounts), accountPicks(accounts)
	e.Nudge = out.Nudge{Lead: "Play it as the default account instead with", Command: "shulker instance unset account"}
	return "", e
}

// launchArgv is the argv with this launch's own settings in it: the memory and JVM arguments after
// the version's own, and the window through the arguments the version declares for a custom
// resolution, as is the quick play target. A version from before those were declared takes the
// pairs appended, as Prism does.
func launchArgv(v game.Version, vars map[string]string, s instance.Settings, window string, target quickPlay) []string {
	var extra []string
	if s.Memory != "" {
		extra = append(extra, "-Xms"+s.Memory, "-Xmx"+s.Memory)
	}
	extra = append(extra, s.JvmArgs...)
	features := map[string]bool{}
	vars = maps.Clone(vars)
	legacy := target.apply(v, features, vars)
	width, height, sized := strings.Cut(window, "x")
	if sized {
		features["has_custom_resolution"] = true
		vars["resolution_width"], vars["resolution_height"] = width, height
	}
	argv := game.Argv(v, game.Host(), features, vars, extra...)
	if sized && !slices.Contains(argv, "--width") {
		argv = append(argv, "--width", width, "--height", height)
	}
	return append(argv, legacy...)
}

// gameSession is the account as the game's own arguments name it. An offline account presents the
// placeholder token Prism uses: no offline-mode host looks at it, and no online one would take a
// real one from an account that has none.
func gameSession(acc account.Account) game.Session {
	s := game.Session{Name: acc.Name(), UUID: acc.ID()}
	if acc.Type == account.Offline {
		s.Token, s.Type = "0", "offline"
		return s
	}
	s.ClientID, s.Type = account.ClientID, "msa"
	if acc.Minecraft != nil {
		s.Token = acc.Minecraft.Token
	}
	if acc.Xbox != nil {
		s.XUID = acc.Xbox.XUID
	}
	return s
}

// launchLog is where a run's output goes. Every launch gets a file of its own, named for when it
// started, because a detached game has no terminal to write to and the last run's output is what
// says why it stopped. Two launches in the same second are still two runs, so the second takes a
// suffix, the way a history entry does.
func launchLog(dir string, at time.Time) (string, error) {
	logs := filepath.Join(dir, instance.Dir, "logs")
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

func (a *app) dryRun(ctx context.Context, args []string, target quickPlay) error {
	in, err := a.playInstance(args)
	if err != nil {
		return err
	}
	p, err := a.playProject(in.Dir)
	if err != nil {
		return err
	}
	if err := a.checkQuickPlay(ctx, p, target); err != nil {
		return err
	}
	plan, err := a.assemble(ctx, in, p)
	if err != nil {
		return err
	}
	rep := playReport{
		Instance:       in.ID,
		Version:        plan.versionID,
		Inherits:       plan.inherits,
		MainClass:      plan.version.MainClass,
		Java:           plan.java,
		GameDir:        in.Dir,
		NativesDir:     plan.natives,
		Classpath:      len(plan.assembly.Libraries) + 1,
		ClasspathBytes: plan.assembly.ClasspathSize(plan.store),
	}
	if plan.version.AssetIndex != nil {
		rep.AssetIndex = plan.version.AssetIndex.ID
	}
	if plan.inherits != "" {
		own := plan.assembly.LibrariesFrom(plan.top, game.Host())
		rep.LoaderLibraries, rep.LoaderLibrariesBytes = len(own), plan.store.Size(own)
	}
	return a.printer.Emit(rep, func(l *out.Lines) {
		l.OK("would launch "+rep.Instance, rep.Version)
		rows := []out.Row{}
		if rep.Inherits != "" {
			rows = append(rows, out.Row{Label: "inherits", Text: rep.Inherits})
		}
		rows = append(rows, out.Row{Label: "main class", Text: rep.MainClass})
		if rep.Inherits != "" {
			rows = append(rows, out.Row{Label: "loader libraries", Text: plural(rep.LoaderLibraries, "jar", "jars") + ", " + out.HumanBytes(rep.LoaderLibrariesBytes)})
		}
		rows = append(rows,
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

// launchPlan is everything a launch needs that doesn't depend on who is playing: the version it
// runs, the store filled with what that version names, the natives unpacked, and the java to run
// it with. A dry run prints it; a launch templates the account into it and starts the game.
type launchPlan struct {
	store     game.Store
	versionID string
	inherits  string
	top       game.Version
	version   game.Version
	assembly  game.Assembly
	natives   string
	java      string
}

func (a *app) assemble(ctx context.Context, in config.Instance, p *project.Project) (launchPlan, error) {
	s, err := a.gameStore()
	if err != nil {
		return launchPlan{}, err
	}
	versionID, err := a.storeVersion(ctx, p, s)
	if err != nil {
		return launchPlan{}, err
	}
	top, err := s.Version(versionID)
	if err != nil {
		return launchPlan{}, err
	}
	v, err := s.Resolve(versionID)
	if err != nil {
		return launchPlan{}, err
	}
	host := game.Host()
	assembly, err := game.Assemble(v, host, nil)
	if err != nil {
		return launchPlan{}, err
	}
	if err := a.fillStore(ctx, s, assembly); err != nil {
		return launchPlan{}, err
	}
	natives := nativesDir(in.Dir)
	if err := assembly.ExtractNatives(s, natives, host); err != nil {
		return launchPlan{}, err
	}
	java, err := a.clientJava(ctx, p, in.Dir)
	if err != nil {
		return launchPlan{}, err
	}
	return launchPlan{store: s, versionID: versionID, inherits: top.InheritsFrom, top: top, version: v, assembly: assembly, natives: natives, java: java}, nil
}

// playInstance is the instance a launch acts on: the nickname given, else the one the current
// directory is. Only the instances shulker owns can be launched from here; every other launcher
// starts its own.
func (a *app) playInstance(args []string) (config.Instance, error) {
	if len(args) == 1 {
		if a.instance != "" || a.dir != "" {
			return config.Instance{}, out.Errorf("usage", "pass a nickname, -i or -C, not more than one: each of them says which instance to launch")
		}
		a.instance = args[0]
	}
	dir, err := a.scopeDir()
	if err != nil {
		return config.Instance{}, err
	}
	in, ok := a.registeredInstance(dir)
	if !ok {
		return config.Instance{}, out.Errorf("instance-not-found", "%s is not a registered instance; `shulker link shulker` makes one shulker launches itself", dir)
	}
	if in.Launcher != "shulker" {
		return config.Instance{}, out.Errorf("not-shulker", "%s belongs to %s, which starts it itself; shulker only launches the instances it owns", in.ID, in.Launcher)
	}
	return in, nil
}

// playProject is the pack a launch runs, read after the sync that may have changed it.
func (a *app) playProject(dir string) (*project.Project, error) {
	p, err := a.openProjectAt(dir)
	if err != nil {
		return nil, err
	}
	return p, a.requireLock(p)
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
// is laid out as the Mojang launcher directory the installer expects. The store remembers the id
// each loader wrote, so a later launch needs neither the installer nor the network.
func (a *app) storeVersion(ctx context.Context, p *project.Project, s game.Store) (string, error) {
	d, err := a.deps()
	if err != nil {
		return "", err
	}
	if err := a.storeVanillaVersion(ctx, s, p.Lock.Minecraft); err != nil {
		return "", err
	}
	if p.Lock.Loader.Type == "" {
		return p.Lock.Minecraft, nil
	}
	l, err := loader.Require(p.Lock.Loader.Type)
	if err != nil {
		return "", err
	}
	key := p.Lock.Loader.Type + "-" + p.Lock.Loader.Version + "-" + p.Lock.Minecraft
	if id, ok := s.InstalledLoader(key); ok && s.HasVersion(id) {
		return id, nil
	}
	var id string
	if l.InstallClientFlag != "" {
		if err := a.fetchVanillaClient(ctx, s, p.Lock.Minecraft); err != nil {
			return "", err
		}
		id, err = a.installClientLoader(ctx, p, &launcher.Mojang{Dir: s.Root}, l)
	} else {
		a.progress("fetching %s loader %s for %s", p.Lock.Loader.Type, p.Lock.Loader.Version, p.Lock.Minecraft)
		var profile []byte
		if profile, err = d.meta.LoaderProfile(ctx, p.Lock.Loader, p.Lock.Minecraft); err == nil {
			id, err = s.SaveVersion(profile)
		}
	}
	if err != nil {
		return "", err
	}
	return id, s.RecordLoader(key, id)
}

// storeVanillaVersion puts the vanilla version JSON for the Minecraft version in the store, unless
// it is there already.
func (a *app) storeVanillaVersion(ctx context.Context, s game.Store, minecraft string) error {
	if s.HasVersion(minecraft) {
		return nil
	}
	d, err := a.deps()
	if err != nil {
		return err
	}
	a.progress("fetching the minecraft %s version json", minecraft)
	raw, err := d.meta.Piston.Version(ctx, minecraft)
	if err != nil {
		return err
	}
	_, err = s.SaveVersion(raw)
	return err
}

// fetchVanillaClient puts the vanilla client jar in the store before a loader's installer runs.
// The installer patches that jar and would download it itself when it is missing, but silently,
// with no progress line of its own.
func (a *app) fetchVanillaClient(ctx context.Context, s game.Store, minecraft string) error {
	v, err := s.Version(minecraft)
	if err != nil {
		return err
	}
	client, err := game.ClientJar(v)
	if err != nil {
		return err
	}
	return a.fetchInto(ctx, s, []game.File{client}, "client jar", "client jars")
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

// clientJava is what the launch runs: the `java` setting when the instance or play.java has one,
// and otherwise shulker's managed runtime for the component the lock names.
func (a *app) clientJava(ctx context.Context, p *project.Project, dir string) (string, error) {
	s, err := a.launchSettings(dir)
	if err != nil {
		return "", err
	}
	if s.Java != "" {
		java, err := server.ClientJava(s.Java, p.Lock.Java.Major)
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
