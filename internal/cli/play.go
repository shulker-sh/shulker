package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/play"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/sync"
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
	Memory         string `json:"memory"`
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
	// game is the Minecraft and loader the launch runs, "Minecraft 26.3, Fabric 0.19.5", and lasted
	// how long a waited run took.
	game   string
	lasted time.Duration
}

func (a *app) playCmd() *cobra.Command {
	var opts playOptions
	cmd := &cobra.Command{
		Use:         "play [nickname]",
		Annotations: acts(),
		Short:       "Start a shulker instance",
		Long:        "Start a shulker instance: the nickname given, the one -i names, or the one the current directory is. In a project folder it plays the instance shulker owns for that project; with several it asks which, and with none it offers to create one on a terminal.",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.window != "" {
				if err := config.CheckPlaySetting("--window", "window", opts.window); err != nil {
					return err
				}
			}
			target, err := parseQuickPlay(cmd, opts.world, opts.server)
			if err != nil {
				return err
			}
			opts.target = target
			if opts.dryRun {
				return a.dryRun(cmd, args, target)
			}
			return a.play(cmd, args, opts)
		},
	}
	a.scopeFlags(cmd)
	a.yesFlag(cmd, "create a shulker instance for a project that has none without being asked first")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "assemble the launch and print it instead of starting the game")
	cmd.Flags().BoolVar(&opts.noSync, "no-sync", false, "start the game without updating the instance first")
	cmd.Flags().BoolVar(&opts.wait, "wait", false, "wait for the game and record how the run ended before returning")
	cmd.Flags().BoolVar(&opts.stream, "stream", false, "wait for the game and mirror its output to the terminal")
	cmd.Flags().StringVar(&opts.account, "account", "", "play as this account (default: the instance's pinned account, else the default account)")
	cmd.Flags().StringVar(&opts.window, "window", "", "open the game at this size for this run, like 1280x720")
	cmd.Flags().StringVar(&opts.world, "world", "", "boot straight into this save, named by its folder in saves/")
	cmd.Flags().StringVar(&opts.server, "server", "", "join this server straight away, as <address>[:<port>]")
	a.registerEveryFetch(cmd)
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
	target  game.QuickPlay
}

// waits reports whether this command stays for the run. --stream is --wait that shows its working,
// so either of them keeps the launch in the foreground.
func (p playOptions) waits() bool { return p.wait || p.stream }

func (a *app) play(cmd *cobra.Command, args []string, opts playOptions) error {
	ctx := cmd.Context()
	in, linked, err := a.playInstance(cmd, args, true)
	if err != nil {
		return err
	}
	pe, err := a.playEnv()
	if err != nil {
		return err
	}
	plan, err := play.Assemble(ctx, pe, in, play.Request{Sync: linked == nil && !opts.noSync, Reason: cmd.Name(), Target: opts.target})
	if err != nil {
		return a.lastOf(err)
	}
	var synced *syncResult
	if plan.Sync != nil {
		res, err := a.synced(*plan.Sync, syncRequest{Request: sync.Request{Into: in.Dir}}, nil)
		if err != nil {
			return err
		}
		synced = &res
	}
	who, adopted, err := play.Account(pe, plan, opts.account)
	if err != nil {
		return err
	}
	if adopted {
		if _, err := a.changeDefault(who.ID); err != nil {
			return err
		}
	}
	signed, err := play.Session(ctx, pe, who)
	if err != nil {
		return err
	}
	// Read back after resolving, so the row says the account is the default one when resolving it
	// is what made it so.
	_, cfg, err := a.accounts()
	if err != nil {
		return err
	}
	launch, err := plan.Launch(pe, signed, opts.window, time.Now())
	if err != nil {
		return err
	}
	res := playResult{
		Instance: in.ID,
		Version:  plan.Launchable.ID,
		Account:  rowFor(who, cfg),
		GameDir:  in.Dir,
		Log:      launch.Log,
		Sync:     synced,
		game:     launchGame(plan.Project.Lock),
	}
	a.printer.Working("launching %s as %s", in.ID, who.Name)
	if opts.waits() {
		var rec instance.Launch
		began := time.Now()
		if res.PID, rec, err = a.playWaited(launch, opts.stream, res.launched()); err != nil {
			return err
		}
		res.Outcome, res.ExitCode, res.CrashReport = rec.Outcome, rec.ExitCode, rec.CrashReport
		res.lasted = time.Since(began)
	} else if res.PID, err = a.startWatcher(launch); err != nil {
		return err
	}
	return a.printer.Emit(res, func(l *out.Lines) {
		if linked != nil {
			linked.print(l)
		}
		if synced != nil && !synced.isIdle(l) {
			synced.print(l)
		}
		if adopted {
			l.Info(who.Name + " is the default account now")
		}
		res.print(l)
	})
}

// playEnv is the play module's env: the sync env, the store the launch assembles in, and the
// config.json, sign-in and picker a launch resolves its account with.
func (a *app) playEnv() (*play.Env, error) {
	if a.pe != nil {
		return a.pe, nil
	}
	se, err := a.syncEnv()
	if err != nil {
		return nil, err
	}
	d, err := a.deps()
	if err != nil {
		return nil, err
	}
	r, err := a.roots()
	if err != nil {
		return nil, err
	}
	path, err := a.configFile()
	if err != nil {
		return nil, err
	}
	a.pe = &play.Env{
		Env:        se,
		Store:      game.Store{Root: r.Store, Resources: d.resources},
		Config:     path,
		SignIn:     d.signin,
		Version:    a.build().Version,
		AskAccount: a.accountPicker(),
	}
	return a.pe, nil
}

// playWaited keeps the launch in the foreground: this command starts the game, waits for it, and
// closes the record itself, so no watcher is spawned and nothing is left running behind it. It hands
// back the game's pid and the record of how the run ended.
func (a *app) playWaited(launch game.Launch, stream bool, launched string) (int, instance.Launch, error) {
	var mirror io.Writer
	if stream {
		mirror = a.printer.Stdout
		if a.printer.JSON {
			mirror = a.printer.Stderr
		}
	}
	a.printer.Settle()
	var pid int
	rec := a.watchRun(launch, mirror, func(r watchReply) {
		pid = r.PID
		if pid != 0 && !a.printer.JSON {
			a.printer.Err().OK(launched, "")
			// The game's own output streams under the line, so it can't be replaced in place.
			if stream {
				a.printer.Err().Pending("Waiting for Minecraft to close")
			} else {
				a.printer.Pending("Waiting for Minecraft to close")
			}
		}
	})
	if rec.Outcome == instance.OutcomeNotStarted {
		return 0, rec, notStarted(rec.Error)
	}
	return pid, rec, nil
}

// print is the launch as a player reads it. A detached launch says the game launched, because that
// is all it knows; a run this command waited for already said so, and says how it went instead. A
// crash is reported rather than raised: the game ran, so shulker did its job.
func (p playResult) print(l *out.Lines) {
	rows := []out.Row{{Label: "log", Text: p.Log}}
	switch p.Outcome {
	case "":
		l.OK(p.launched(), "")
		rows = append(rows, out.Row{Label: "pid", Text: strconv.Itoa(p.PID)})
	case instance.OutcomeCrashed:
		crashed := "Minecraft crashed"
		if p.ExitCode != 0 {
			crashed += fmt.Sprintf(" (exit code %d)", p.ExitCode)
		}
		l.Failed(crashed)
		if p.CrashReport != "" {
			rows = append(rows, out.Row{Label: "crash report", Text: p.CrashReport})
		}
	default:
		l.OK("Minecraft closed after "+runLength(p.lasted), "")
	}
	l.Tree(rows...)
	if p.Outcome == "" {
		l.Nudge("If Minecraft freezes while it's running, dump its threads", "shulker instance dump -i "+p.Instance)
	}
}

func (p playResult) launched() string {
	return fmt.Sprintf("Launched %s as %s (%s)", p.Instance, p.Account.Name, p.game)
}

// launchGame is the Minecraft version, then the loader with its version when there is one:
// "Minecraft 26.3, Fabric 0.19.5".
func launchGame(l *lock.Lock) string {
	game := "Minecraft " + l.Minecraft
	if l.Loader.Type != "" {
		game += ", " + strings.TrimSpace(loader.Title(l.Loader.Type)+" "+l.Loader.Version)
	}
	return game
}

// runLength is how long a run took, to the second: "4s", "12m 4s", "1h 3m".
func runLength(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func (a *app) dryRun(cmd *cobra.Command, args []string, target game.QuickPlay) error {
	in, _, err := a.playInstance(cmd, args, false)
	if err != nil {
		return err
	}
	pe, err := a.playEnv()
	if err != nil {
		return err
	}
	plan, err := play.Assemble(cmd.Context(), pe, in, play.Request{Target: target})
	if err != nil {
		return err
	}
	l := plan.Launchable
	rep := playReport{
		Instance:       in.ID,
		Version:        l.ID,
		Inherits:       l.Top.InheritsFrom,
		MainClass:      l.Version.MainClass,
		Java:           plan.Java,
		Memory:         plan.Settings.Memory,
		GameDir:        in.Dir,
		NativesDir:     plan.Natives,
		Classpath:      len(l.Assembly.Libraries) + 1,
		ClasspathBytes: l.Assembly.ClasspathSize(pe.Store),
	}
	if l.Version.AssetIndex != nil {
		rep.AssetIndex = l.Version.AssetIndex.ID
	}
	if rep.Inherits != "" {
		own := l.Assembly.LibrariesFrom(l.Top, plan.Platform)
		rep.LoaderLibraries, rep.LoaderLibrariesBytes = len(own), pe.Store.Size(own)
	}
	return a.printer.Emit(rep, func(l *out.Lines) {
		l.OK("Would launch "+rep.Instance, rep.Version)
		rows := []out.Row{}
		if rep.Inherits != "" {
			rows = append(rows, out.Row{Label: "inherits", Text: rep.Inherits})
		}
		rows = append(rows, out.Row{Label: "main class", Text: rep.MainClass})
		if rep.Inherits != "" {
			rows = append(rows, out.Row{Label: "loader libraries", Text: out.Count(rep.LoaderLibraries, "jar", "jars") + ", " + out.HumanBytes(rep.LoaderLibrariesBytes)})
		}
		rows = append(rows,
			out.Row{Label: "java", Text: rep.Java},
			out.Row{Label: "memory", Text: rep.Memory},
			out.Row{Label: "game dir", Text: rep.GameDir},
			out.Row{Label: "natives", Text: rep.NativesDir},
		)
		if rep.AssetIndex != "" {
			rows = append(rows, out.Row{Label: "asset index", Text: rep.AssetIndex})
		}
		rows = append(rows, out.Row{Label: "classpath", Text: out.Count(rep.Classpath, "jar", "jars") + ", " + out.HumanBytes(rep.ClasspathBytes)})
		l.Tree(rows...)
	})
}

// playInstance is the instance a launch acts on: the nickname given, else the one the current
// directory is, else the one shulker owns for the project the current directory holds. Only the
// instances shulker owns can be launched from here; every other launcher starts its own. With
// create, a project nothing plays yet asks to make its instance, which is linked and synced here,
// and returned with that link's report.
func (a *app) playInstance(cmd *cobra.Command, args []string, create bool) (config.Instance, *linkReport, error) {
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
	if !ok && a.instance == "" {
		return a.projectInstance(cmd, dir, create)
	}
	if !ok {
		return config.Instance{}, nil, notRegistered(dir)
	}
	if !launcher.Shulker.Launches(in) {
		e := out.Errorf("not-shulker", "%s belongs to %s, which starts it itself", in.ID, in.Launcher)
		e.Help = "shulker only launches the instances it owns"
		return config.Instance{}, nil, e
	}
	return in, nil, nil
}

func notRegistered(dir string) error {
	e := out.Errorf("instance-not-found", "%s is not a registered instance", dir)
	e.Help = "`shulker link shulker` makes one shulker launches itself"
	return e
}

// projectInstance is the instance shulker owns for the project in dir. Instances other launchers
// own are synced from it too, but never count, since shulker doesn't start them.
func (a *app) projectInstance(cmd *cobra.Command, dir string, create bool) (config.Instance, *linkReport, error) {
	p, err := a.openProjectAt(dir)
	if errors.Is(err, project.ErrNoManifest) {
		return config.Instance{}, nil, notRegistered(dir)
	}
	if err != nil {
		return config.Instance{}, nil, err
	}
	registry, err := a.loadInstances()
	if err != nil {
		return config.Instance{}, nil, err
	}
	source, err := filepath.Abs(p.Dir)
	if err != nil {
		return config.Instance{}, nil, err
	}
	own := launcher.Owned(registry, source)
	switch len(own) {
	case 0:
		return a.createProjectInstance(cmd, p, dir, create)
	case 1:
		a.logInstance(own[0].ID)
		return own[0], nil, nil
	}
	entries := make([]project.InstanceEntry, len(own))
	for i, in := range own {
		entries[i] = project.Inspect(in)
	}
	project.SortInstances(entries)
	t := a.printer.ErrTheme
	e, err := pickOne(a, "Play which one?", entries,
		func(e project.InstanceEntry) string { return e.ID },
		func(e project.InstanceEntry) string { return instancePickLabel(t, e) },
		func() error {
			e := out.Errorf("ambiguous-instance", "several shulker instances play %s", p.Manifest.DisplayName("client"))
			e.Help = "pass -i <id> to choose one"
			e.Candidates, e.Pass, e.Flag = instanceCandidates(entries), instanceIDs(entries), "--instance"
			return e
		})
	if err != nil {
		return config.Instance{}, nil, err
	}
	a.logInstance(e.ID)
	return e.Instance, nil, nil
}

func (a *app) createProjectInstance(cmd *cobra.Command, p *project.Project, dir string, create bool) (config.Instance, *linkReport, error) {
	if !create || !a.asksYes() {
		return config.Instance{}, nil, notRegistered(dir)
	}
	yes, err := a.askYes("Create a shulker instance for " + p.Manifest.DisplayName("client") + " and play it?")
	if err != nil {
		return config.Instance{}, nil, err
	}
	if !yes {
		return config.Instance{}, nil, notRegistered(dir)
	}
	rep, err := a.linkInto(cmd, nil, launcher.Shulker, &launcherLink{})
	if err != nil {
		return config.Instance{}, nil, err
	}
	in, ok := a.registeredInstance(rep.GameDir)
	if !ok {
		return config.Instance{}, nil, notRegistered(rep.GameDir)
	}
	return in, rep, nil
}
