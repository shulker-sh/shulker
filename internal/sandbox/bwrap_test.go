package sandbox

import (
	"slices"
	"strings"
	"testing"
)

func TestBwrapArgsHideHomeAndBindThePolicyBack(t *testing.T) {
	p := Policy{
		Read:         []string{"/home/me/.cache/shulker/java/21"},
		ReadFiles:    []string{"/home/me/.cache/shulker/libraries/lwjgl.jar"},
		Write:        []string{"/home/me/instances/smp"},
		Protect:      []string{"/home/me/instances/smp/mods", "/home/me/instances/smp/.shulker"},
		ProtectFiles: []string{"/home/me/instances/smp/instance.json"},
		Reopen:       []string{"/home/me/instances/smp/.shulker/logs"},
	}
	d := Desktop{Home: "/home/me", Hidden: []string{"/mnt"}, Runtime: "/run/user/1000", Sockets: []string{"wayland-0", "pipewire-0"}, Xauthority: "/home/me/.Xauthority"}
	got := strings.Join(BwrapArgs(p, d), " ")
	order := []string{
		"--ro-bind / / --dev-bind /dev /dev --tmpfs /dev/shm",
		"--tmpfs /tmp --ro-bind-try /tmp/.X11-unix /tmp/.X11-unix",
		"--tmpfs /home/me --tmpfs /mnt",
		"--tmpfs /run/user/1000 --bind-try /run/user/1000/wayland-0 /run/user/1000/wayland-0 --bind-try /run/user/1000/pipewire-0 /run/user/1000/pipewire-0",
		"--ro-bind-try /home/me/.Xauthority /home/me/.Xauthority",
		"--ro-bind-try /home/me/.cache/shulker/java/21 /home/me/.cache/shulker/java/21",
		"--ro-bind-try /home/me/.cache/shulker/libraries/lwjgl.jar /home/me/.cache/shulker/libraries/lwjgl.jar",
		"--bind /home/me/instances/smp /home/me/instances/smp",
		"--ro-bind /home/me/instances/smp/mods /home/me/instances/smp/mods",
		"--ro-bind /home/me/instances/smp/.shulker /home/me/instances/smp/.shulker",
		"--ro-bind /home/me/instances/smp/instance.json /home/me/instances/smp/instance.json",
		"--bind /home/me/instances/smp/.shulker/logs /home/me/instances/smp/.shulker/logs",
		"--unshare-pid --new-session --die-with-parent --unsetenv DBUS_SESSION_BUS_ADDRESS --",
	}
	at := 0
	for _, part := range order {
		i := strings.Index(got[at:], part)
		if i < 0 {
			t.Fatalf("arguments lack, or have out of order:\n%s\n\nin:\n%s", part, got)
		}
		at += i + len(part)
	}
	if strings.Contains(got, "/run/user/1000/bus") {
		t.Fatal("the session bus must stay out of the sandbox")
	}
	if args := BwrapArgs(p, Desktop{Home: "/home/me"}); slices.Contains(args, "") {
		t.Fatalf("an unset runtime folder or Xauthority adds no empty argument: %q", args)
	}
}
