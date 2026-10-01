package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// otherVolumes are where a Linux system mounts other disks and other people's homes.
var otherVolumes = []string{"/home", "/root", "/mnt", "/media", "/run/media"}

// Available is nil when this machine can sandbox a game, and otherwise says why not. bubblewrap
// has to be installed and has to be able to start: Ubuntu from 24.04 keeps a program from making
// the user namespace it needs unless an AppArmor profile lets it.
func Available() error {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return errors.New("bubblewrap (bwrap) isn't installed")
	}
	if err := exec.Command(path, "--ro-bind", "/", "/", "true").Run(); err != nil {
		return errors.New("bubblewrap can't start a sandbox here; on Ubuntu 24.04 and later it needs an AppArmor profile that lets bwrap create user namespaces")
	}
	return nil
}

// Exec replaces this process with Java under the policy, and only returns when that fails.
// Everything Java starts stays inside the same mounts.
func Exec(p Policy, java string, argv []string) error {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return err
	}
	if err := makeBound(p); err != nil {
		return err
	}
	args := append([]string{path}, BwrapArgs(p, desktop())...)
	// Landlock restricts the thread that asks, and exec carries that thread's restrictions on.
	runtime.LockOSThread()
	scopeSockets(path)
	return syscall.Exec(path, append(append(args, java), argv...), os.Environ())
}

// scopeSockets keeps what this thread execs from connecting to an abstract socket made outside it.
// Those live in the network namespace, which the sandbox shares for multiplayer, so no mount hides
// them, and an older session bus is one. It is Landlock's scoping, from Linux 6.12; an older kernel
// has none and the sockets stay reachable. A domain that scopes and handles no file access leaves
// mounting alone, so bubblewrap still builds its own. It needs no_new_privs, which a setuid
// bubblewrap couldn't run under, so one of those is left as it is.
func scopeSockets(bwrap string) bool {
	if info, err := os.Stat(bwrap); err != nil || info.Mode()&os.ModeSetuid != 0 {
		return false
	}
	attr := unix.LandlockRulesetAttr{Scoped: unix.LANDLOCK_SCOPE_ABSTRACT_UNIX_SOCKET}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return false
	}
	defer unix.Close(int(fd))
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return false
	}
	_, _, errno = unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0)
	return errno == 0
}

// makeBound makes the folders the policy binds for writing, keeps read-only or reopens: one that
// isn't there can't be bound, and the game could then make a read-only folder itself, writable.
func makeBound(p Policy) error {
	for _, dir := range slices.Concat(p.Write, p.Protect, p.Reopen) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// desktop reads the session this process runs in.
func desktop() Desktop {
	d := Desktop{Runtime: os.Getenv("XDG_RUNTIME_DIR"), Sockets: runtimeSockets}
	d.Home, _ = os.UserHomeDir()
	d.Home = resolve(d.Home)
	// A folder that holds the home is left as it is: the home inside it is hidden on its own, and
	// other people's homes there are already theirs alone.
	for _, dir := range otherVolumes {
		if info, err := os.Stat(dir); err == nil && info.IsDir() && !within(d.Home, dir) {
			d.Hidden = append(d.Hidden, dir)
		}
	}
	if display := os.Getenv("WAYLAND_DISPLAY"); display != "" {
		d.Sockets = append([]string{display}, d.Sockets...)
	}
	d.Xauthority = os.Getenv("XAUTHORITY")
	if d.Xauthority == "" && d.Home != "" {
		d.Xauthority = filepath.Join(d.Home, ".Xauthority")
	}
	return d
}
