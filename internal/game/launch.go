package game

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// Argv is everything java is handed for one launch: the version's JVM arguments, the main class it
// names, then the game's own arguments. It carries the session's access token, so what comes back
// is never printed, logged or recorded.
func Argv(v Version, p Platform, features map[string]bool, vars map[string]string) []string {
	jvm, game := Args(v, p, features, vars)
	argv := make([]string, 0, len(jvm)+1+len(game))
	argv = append(argv, jvm...)
	argv = append(argv, v.MainClass)
	return append(argv, game...)
}

// Launch is one start of the game: the java that runs it, the argv it runs with, the directory it
// runs in, and the file every byte it writes goes to.
type Launch struct {
	Java string
	Argv []string
	Dir  string
	Log  string
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
	err := g.cmd.Wait()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	}
	return 0, err
}
