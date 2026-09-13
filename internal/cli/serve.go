package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/server"
)

const eulaURL = "https://aka.ms/MinecraftEULA"

type serveResult struct {
	Target      string      `json:"target"`
	Dir         string      `json:"dir"`
	Java        server.Java `json:"java"`
	Args        []string    `json:"args"`
	ExitCode    int         `json:"exitCode"`
	Log         string      `json:"log,omitempty"`
	CrashReport string      `json:"crashReport,omitempty"`
}

// serverFailureFiles returns the server's log and the newest crash report written since started,
// each empty when there isn't one.
func serverFailureFiles(dir string, started time.Time) (log, crashReport string) {
	if path := filepath.Join(dir, "logs", "latest.log"); fileExists(path) {
		log = path
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "crash-reports"))
	var newest time.Time
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() || info.ModTime().Before(started) || !info.ModTime().After(newest) {
			continue
		}
		newest = info.ModTime()
		crashReport = filepath.Join(dir, "crash-reports", e.Name())
	}
	return log, crashReport
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (a *app) projectJava(ctx context.Context, p *project.Project) (server.Java, error) {
	if p.Manifest.Java != "" {
		return server.FindJava(p.Manifest.Java, p.Lock.Java.Major)
	}
	rt, err := a.managedJava(ctx, p, false)
	if err != nil {
		if out.CodeOf(err) != "runtime-unavailable" {
			return server.Java{}, err
		}
		a.printer.Warn("%s; using java on PATH", err)
		return server.FindJava("", p.Lock.Java.Major)
	}
	return server.JavaAt(rt.Home)
}

func (a *app) serveCmd() *cobra.Command {
	var tf targetFlag
	var force, acceptEula bool
	cmd := &cobra.Command{
		Use:   "serve [target]",
		Short: "Build a server target and run it in the foreground",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := tf.resolve(args)
			if err != nil {
				return err
			}
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := a.requireLock(p); err != nil {
				return err
			}
			name, err := sideTarget(p.Manifest, target, "server", "serve")
			if err != nil {
				return err
			}
			var in io.Reader = a.stdin
			if in == nil {
				in = strings.NewReader("")
			}
			stdin := bufio.NewReader(in)
			if p.Manifest.Server == nil {
				p.Manifest.Server = &manifest.Server{}
			}
			srv := p.Manifest.Server
			if !srv.Eula {
				accepted, err := a.acceptEula(stdin, acceptEula)
				if err != nil {
					return err
				}
				if !accepted {
					return out.Errorf("eula-required", "set \"server\": {\"eula\": true} in shulker.json or pass --accept-eula once you accept the Minecraft EULA (%s)", eulaURL)
				}
				srv.Eula = true
				if err := p.SaveManifest(); err != nil {
					return err
				}
			}
			jvm, err := server.JVMArgs(srv.Memory, srv.JvmFlags, srv.JvmArgs)
			if err != nil {
				return err
			}
			java, err := a.projectJava(cmd.Context(), p)
			if err != nil {
				return err
			}
			if _, err := a.fetchLocked(cmd.Context(), p, true); err != nil {
				return err
			}
			if err := a.syncPlayers(cmd.Context(), p, player.MissingOnly, false, true); err != nil {
				return err
			}
			b, err := a.builder(cmd.Context(), p)
			if err != nil {
				return err
			}
			rep, err := b.Build(name, build.Options{Force: force})
			if err != nil {
				return err
			}
			if err := a.installServerLoader(cmd.Context(), p, rep); err != nil {
				return err
			}
			a.printer.Err().Muted(rep.Summary())
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			dir := filepath.Join(p.Dir, p.Manifest.BuildDir(name))
			launchArgs := server.Command(jvm, build.LaunchArgs(p.Lock))
			a.progress("starting %s in %s with %s", name, dir, java)
			a.printer.Settle()

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
				Stdin:     stdin,
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
			res := serveResult{Target: name, Dir: dir, Java: java, Args: launchArgs, ExitCode: code}
			if code != 0 {
				res.Log, res.CrashReport = serverFailureFiles(dir, started)
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
				l.OK("server stopped", "")
			})
		},
	}
	tf.register(cmd, "server target to run (default: the only server target)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the build directory")
	cmd.Flags().BoolVar(&acceptEula, "accept-eula", false, "record acceptance of the Minecraft EULA in shulker.json without prompting")
	return cmd
}

func (a *app) acceptEula(stdin *bufio.Reader, flag bool) (bool, error) {
	if flag {
		return true, nil
	}
	if a.printer.JSON || a.tty == nil || !a.tty() {
		return false, nil
	}
	l := a.printer.Err()
	l.Text("Running a Minecraft server requires accepting the EULA: " + l.T.Cyan(eulaURL))
	fmt.Fprint(a.printer.Stderr, "  Accept and record \"eula\": true in shulker.json? "+l.T.Grey("[y/N]")+" ")
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}
