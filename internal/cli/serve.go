package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/java"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/server"
	"shulker.sh/shulker/internal/sync"
)

const eulaURL = "https://aka.ms/MinecraftEULA"

type serveResult struct {
	Side        string      `json:"side"`
	Dir         string      `json:"dir"`
	Java        java.Binary `json:"java"`
	Args        []string    `json:"args"`
	ExitCode    int         `json:"exitCode"`
	Log         string      `json:"log,omitempty"`
	CrashReport string      `json:"crashReport,omitempty"`
}

func (a *app) projectJava(ctx context.Context, p *project.Project) (java.Binary, error) {
	se, err := a.syncEnv()
	if err != nil {
		return java.Binary{}, err
	}
	return sync.ProjectJava(ctx, se, p)
}

func (a *app) serveCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:         "serve",
		Annotations: acts(),
		Short:       "Build the server side and run it in the foreground",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := a.requireLock(p); err != nil {
				return err
			}
			if !p.Manifest.HasSide("server") {
				return project.NoSide("server")
			}
			var in io.Reader = a.stdin
			if in == nil {
				in = strings.NewReader("")
			}
			if p.Manifest.Server == nil {
				p.Manifest.Server = &manifest.Server{}
			}
			srv := p.Manifest.Server
			jvm, err := server.JVMArgs(srv.Memory, srv.JVMFlags)
			if err != nil {
				return err
			}
			java, err := a.projectJava(cmd.Context(), p)
			if err != nil {
				return err
			}
			if _, err := a.fetchLocked(cmd.Context(), p, nil, true); err != nil {
				return err
			}
			if err := a.syncPlayers(cmd.Context(), p, player.MissingOnly, false, true); err != nil {
				return err
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			rep, err := b.Build("server", build.Options{Force: force})
			if err != nil {
				return err
			}
			if !build.HasEula(rep.Dir) {
				if err := a.requireEula(); err != nil {
					return err
				}
				b.EULA = true
				if rep, err = b.Build("server", build.Options{Force: force}); err != nil {
					return err
				}
			}
			a.warnBuild("server", false, nil, a.securityWarnings(rep), rep.State, takeOver(cmd, nil, force))
			if err := a.installServerLoader(cmd.Context(), p, rep); err != nil {
				return err
			}
			a.printer.Err().OK("Built server", reportAside(rep))
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			dir := rep.Dir
			launchArgs := server.Command(jvm, build.LaunchArgs(p.Lock))
			l := a.printer.Err()
			l.OKInto("Started server with Java "+strconv.Itoa(java.Major), dir, "")
			l.Blank()

			interrupt := make(chan os.Signal, 2)
			signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
			defer signal.Stop(interrupt)
			gameOut := a.printer.Stdout
			if a.printer.JSON {
				gameOut = a.printer.Stderr
			}
			r := &server.Runner{
				Java:      java.Path,
				Dir:       dir,
				Args:      launchArgs,
				Stdin:     in,
				Stdout:    gameOut,
				Stderr:    a.printer.Stderr,
				Interrupt: interrupt,
				Log:       a.printer.Stderr,
			}
			// File times can be coarser than the clock, so a crash report from this run's first second counts.
			started := time.Now().Truncate(time.Second)
			code, err := r.Run()
			if err != nil {
				return err
			}
			res := serveResult{Side: "server", Dir: dir, Java: java, Args: launchArgs, ExitCode: code}
			if code != 0 {
				res.Log, res.CrashReport = instance.FailureFiles(dir, started)
				if a.printer.JSON {
					_ = a.printer.Emit(res, func(*out.Lines) {})
				}
				e := &out.Error{Code: "server-exit", Message: fmt.Sprintf("server exited with status %d", code), Exit: code}
				if res.Log != "" {
					e.Items = append(e.Items, "log: "+res.Log)
				}
				if res.CrashReport != "" {
					e.Items = append(e.Items, "crash report: "+res.CrashReport)
				}
				return e
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				l.Blank()
				l.OK("Server stopped", "")
			})
		},
	}
	a.scopeFlags(cmd)
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the build directory, seeded files included.")
	a.registerFailFast(cmd)
	a.yesFlag(cmd, "accept the Minecraft EULA and record it in config.json without prompting.")
	return cmd
}

// requireEula has this user accept the Minecraft EULA, by the prompt or --yes, and records
// it in config.json. A manifest never accepts it for them.
func (a *app) requireEula() error {
	accepted := a.yes
	if !accepted && a.canPick() {
		l := a.printer.Err()
		l.Text("Running a Minecraft server requires accepting the EULA: " + l.T.Cyan(eulaURL))
		var err error
		if accepted, err = a.askYes(`Accept and record "eula": true in your shulker config?`); err != nil {
			return err
		}
	}
	if !accepted {
		e := out.Errorf("eula-required", "accept the Minecraft EULA (%s) to run a server", eulaURL)
		e.Help = "pass --yes, or run `shulker config set eula true`"
		return e
	}
	path, err := a.configFile()
	if err != nil {
		return err
	}
	return config.AcceptEULA(path)
}
