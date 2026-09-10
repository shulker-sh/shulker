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

	"github.com/andrewmast/shulker/internal/build"
	"github.com/andrewmast/shulker/internal/manifest"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/andrewmast/shulker/internal/player"
	"github.com/andrewmast/shulker/internal/project"
	"github.com/andrewmast/shulker/internal/server"
	"github.com/spf13/cobra"
)

const eulaURL = "https://aka.ms/MinecraftEULA"

type serveResult struct {
	Target   string      `json:"target"`
	Dir      string      `json:"dir"`
	Java     server.Java `json:"java"`
	Args     []string    `json:"args"`
	ExitCode int         `json:"exitCode"`
}

func (a *app) serveJava(ctx context.Context, p *project.Project) (server.Java, error) {
	if p.Manifest.Java != "" {
		return server.FindJava(p.Manifest.Java, p.Lock.Java.Major)
	}
	rt, err := a.managedJava(ctx, p, false)
	if err != nil {
		if out.CodeOf(err) != "runtime-unavailable" {
			return server.Java{}, err
		}
		a.progress("%s; using java on PATH", err)
		return server.FindJava("", p.Lock.Java.Major)
	}
	return server.JavaAt(rt.Home)
}

func (a *app) serveCmd() *cobra.Command {
	var target string
	var force, acceptEula bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Build a server target and run it in the foreground",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := a.openProject()
			if err != nil {
				return err
			}
			if err := p.RequireLock(); err != nil {
				return err
			}
			if a.printer.LockStale && !force {
				return out.Errorf("lock-stale", "shulker.lock does not match shulker.json; run `shulker add`, `remove`, or `update`, or pass --force")
			}
			name, t, err := sideTarget(p.Manifest, target, "server", "serve")
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
			java, err := a.serveJava(cmd.Context(), p)
			if err != nil {
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
			a.progress("%s", rep.Summary())
			dir := filepath.Join(p.Dir, t.Build)
			args := server.Command(jvm, build.ServerJarFile)
			a.progress("starting %s in %s with %s", name, dir, java)

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
				Args:      args,
				Stdin:     stdin,
				Stdout:    gameOut,
				Stderr:    a.printer.Stderr,
				Interrupt: interrupt,
				Log:       a.printer.Stderr,
			}
			code, err := r.Run()
			if err != nil {
				return err
			}
			res := serveResult{Target: name, Dir: dir, Java: java, Args: args, ExitCode: code}
			if code != 0 {
				if a.printer.JSON {
					_ = a.printer.Emit(res, func(io.Writer) {})
				}
				return &out.Error{Code: "server-exit", Message: fmt.Sprintf("server exited with status %d", code), Exit: code}
			}
			return a.printer.Emit(res, func(w io.Writer) {
				fmt.Fprintln(w, "server stopped")
			})
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "server target to run (default: the only server target)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite files edited in the build directory and ignore a stale lock")
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
	fmt.Fprintf(a.printer.Stderr, "Running a Minecraft server requires accepting the EULA: %s\nAccept and record \"eula\": true in shulker.json? [y/N] ", eulaURL)
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
