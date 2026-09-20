package game

import (
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

// Start runs the game and returns as soon as it is running. The process is detached, so it outlives
// the shell that asked for it, and its output goes to the log rather than to a terminal that may
// already be gone by the time the game has anything to say.
func Start(l Launch) (int, error) {
	if err := os.MkdirAll(filepath.Dir(l.Log), 0o755); err != nil {
		return 0, err
	}
	log, err := os.OpenFile(l.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer log.Close()
	cmd := exec.Command(l.Java, l.Argv...)
	cmd.Dir = l.Dir
	cmd.Stdout, cmd.Stderr = log, log
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, cmd.Process.Release()
}
