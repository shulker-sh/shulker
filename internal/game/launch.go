// Package game launches Minecraft directly, from a store laid out the way the Mojang launcher lays
// out its own directory.
package game

import (
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/proc"
)

// Session is who is playing, as the game's own arguments name them. Every field here reaches the
// argv, which is why no part of a launch's argv is ever printed: one of them is a session token.
type Session struct {
	Name     string
	UUID     string
	Token    string
	XUID     string
	ClientID string
	Type     string
}

// SessionOf is the account as the game's own arguments name it. An offline account presents the
// placeholder token Prism uses: no offline-mode host looks at it, and no online one would take a
// real one from an account that has none.
func SessionOf(acc account.Account) Session {
	s := Session{Name: acc.Name(), UUID: acc.ID()}
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

// Vars are the substitutions only the account can fill, added on top of Assembly.Vars. The legacy
// auth_session is the pre-1.6 pair of token and uuid, which a version old enough to want it takes
// in place of the two separate arguments.
func (s Session) Vars() map[string]string {
	return map[string]string{
		"auth_player_name":  s.Name,
		"auth_uuid":         s.UUID,
		"auth_access_token": s.Token,
		"auth_session":      "token:" + s.Token + ":" + s.UUID,
		"auth_xuid":         s.XUID,
		"clientid":          s.ClientID,
		"user_type":         s.Type,
	}
}

// Argv is everything java is handed for one launch: the version's JVM arguments and then extra, the
// main class it names, then the game's own arguments. extra comes after the version's own so that a
// player's -Xmx or -D wins over one the version sets. It carries the session's access token, so
// what comes back is never printed, logged or recorded.
func Argv(v Version, p Platform, features map[string]bool, vars map[string]string, extra ...string) []string {
	jvm, game := Args(v, p, features, vars)
	argv := make([]string, 0, len(jvm)+len(extra)+1+len(game))
	argv = append(argv, jvm...)
	argv = append(argv, extra...)
	argv = append(argv, v.MainClass)
	return append(argv, game...)
}

// LaunchArgv is the argv with one launch's own settings in it: the memory and JVM arguments after
// the version's own, and the window through the arguments the version declares for a custom
// resolution, as is the quick play target. A version from before those were declared takes the
// pairs appended, as Prism does.
func LaunchArgv(v Version, p Platform, vars map[string]string, memory string, jvmArgs []string, window string, target QuickPlay) []string {
	var extra []string
	if memory != "" {
		extra = append(extra, "-Xms"+memory, "-Xmx"+memory)
	}
	extra = append(extra, jvmArgs...)
	features := map[string]bool{}
	vars = maps.Clone(vars)
	legacy := target.Apply(v, features, vars)
	width, height, sized := strings.Cut(window, "x")
	if sized {
		features["has_custom_resolution"] = true
		vars["resolution_width"], vars["resolution_height"] = width, height
	}
	argv := Argv(v, p, features, vars, extra...)
	if sized && !slices.Contains(argv, "--width") {
		argv = append(argv, "--width", width, "--height", height)
	}
	return append(argv, legacy...)
}

// Launch is one start of the game: the java that runs it, the argv it runs with, the directory it
// runs in, and the file every byte it writes goes to.
type Launch struct {
	Java string   `json:"java"`
	Argv []string `json:"argv"`
	Dir  string   `json:"dir"`
	Log  string   `json:"log"`
	// Wrapper is a command the launch runs through, handed java and its argv as its own arguments.
	Wrapper []string `json:"wrapper,omitempty"`
}

// Program is what a launch execs: its wrapper when it has one, else java.
func (l Launch) Program() string {
	if len(l.Wrapper) > 0 {
		return l.Wrapper[0]
	}
	return l.Java
}

// Game is a game that has started: the process to record, and the wait that ends when it exits.
type Game struct {
	PID int

	cmd *exec.Cmd
	log *os.File
}

// Start runs the game and returns as soon as it is running. The game is in a session of its own, so
// it outlives whatever started it — a shell that closed, or a watcher that was killed — and its
// output goes to the log rather than to a terminal that may already be gone by the time the game
// has anything to say. A non-nil stream is handed a copy of that output as it arrives.
func Start(l Launch, stream io.Writer) (*Game, error) {
	if err := os.MkdirAll(filepath.Dir(l.Log), 0o755); err != nil {
		return nil, err
	}
	log, err := os.OpenFile(l.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(l.Java, l.Argv...)
	if len(l.Wrapper) > 0 {
		cmd = exec.Command(l.Wrapper[0], slices.Concat(l.Wrapper[1:], []string{l.Java}, l.Argv)...)
	}
	cmd.Dir = l.Dir
	cmd.Stdout = io.Writer(log)
	if stream != nil {
		cmd.Stdout = io.MultiWriter(log, stream)
	}
	cmd.Stderr = cmd.Stdout
	detach(cmd)
	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, err
	}
	return &Game{PID: cmd.Process.Pid, cmd: cmd, log: log}, nil
}

// Wait blocks until the game exits and hands back the status it left. A game that ran and failed is
// a status, not an error: only a game that never started at all is that.
func (g *Game) Wait() (int, error) {
	defer g.log.Close()
	return proc.ExitCode(g.cmd.Wait())
}
