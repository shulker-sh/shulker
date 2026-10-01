package sandbox

import (
	"path/filepath"
	"slices"
)

// Desktop is what a Linux session has that a game needs and that sits where the sandbox hides
// everything else: its home, its runtime folder, and the sockets and files inside them.
type Desktop struct {
	Home string
	// Hidden are folders besides Home that hold other people's files or other volumes.
	Hidden []string
	// Runtime is XDG_RUNTIME_DIR, hidden whole and given back one socket at a time: it also holds
	// the session bus, which would start a process outside the sandbox for whoever asks.
	Runtime string
	// Sockets are the entries of Runtime the game talks to: the display, the audio and Discord.
	Sockets []string
	// Xauthority is the file an X11 client proves itself with, read-only.
	Xauthority string
}

// runtimeSockets are the entries of XDG_RUNTIME_DIR a game uses, besides the Wayland display the
// session names itself.
var runtimeSockets = []string{
	"pipewire-0", "pulse",
	"discord-ipc-0", "discord-ipc-1", "discord-ipc-2", "discord-ipc-3", "discord-ipc-4",
	"discord-ipc-5", "discord-ipc-6", "discord-ipc-7", "discord-ipc-8", "discord-ipc-9",
}

// BwrapArgs are bubblewrap's arguments for a policy, up to and including the "--" the command
// follows. The whole system is bound read-only, home, the other volumes, /tmp and the runtime
// folder are replaced by empty ones, and what the policy allows is bound back over them, a later
// bind winning over an earlier one. bubblewrap, not Landlock, because only a mount can keep a
// folder read-only inside one the game may write.
func BwrapArgs(p Policy, d Desktop) []string {
	// Shared memory gets a folder of its own: /dev/shm is one every program of the session can write.
	args := []string{"--ro-bind", "/", "/", "--dev-bind", "/dev", "/dev", "--tmpfs", "/dev/shm", "--proc", "/proc"}
	// A private /tmp keeps other programs' sockets there, an ssh-agent's among them, out of reach.
	args = append(args, "--tmpfs", "/tmp", "--ro-bind-try", "/tmp/.X11-unix", "/tmp/.X11-unix")
	for _, dir := range slices.Concat([]string{d.Home}, d.Hidden) {
		if dir != "" {
			args = append(args, "--tmpfs", dir)
		}
	}
	if d.Runtime != "" {
		args = append(args, "--tmpfs", d.Runtime)
		for _, name := range d.Sockets {
			path := name
			if !filepath.IsAbs(path) {
				path = filepath.Join(d.Runtime, name)
			}
			args = append(args, "--bind-try", path, path)
		}
	}
	if d.Xauthority != "" {
		args = append(args, "--ro-bind-try", d.Xauthority, d.Xauthority)
	}
	bind := func(flag string, paths []string) {
		for _, path := range paths {
			args = append(args, flag, path, path)
		}
	}
	// A classpath can name a jar that isn't there, which Java shrugs at and a plain bind would not.
	bind("--ro-bind-try", p.Read)
	bind("--ro-bind-try", p.ReadFiles)
	bind("--bind", p.Write)
	bind("--ro-bind", p.Protect)
	bind("--ro-bind", p.ProtectFiles)
	bind("--bind", p.Reopen)
	// A process namespace of its own keeps the game from seeing, tracing or reading through /proc the
	// player's other programs, which run as the same user outside the sandbox. A new session keeps it
	// from typing into the terminal that started it, and the session bus address is dropped with the
	// bus.
	return append(args, "--unshare-pid", "--new-session", "--die-with-parent", "--unsetenv", "DBUS_SESSION_BUS_ADDRESS", "--")
}
