package launcher

import (
	"errors"
	"testing"

	"shulker.sh/shulker/internal/proc"
)

// No test reads the machine's own processes: a launcher open on it would change what they print.
func init() { Processes = noProcesses }

func noProcesses() ([]proc.Process, error) { return nil, errors.New("no process list in tests") }

func withProcesses(t *testing.T, processes ...proc.Process) {
	t.Helper()
	Processes = func() ([]proc.Process, error) { return processes, nil }
	t.Cleanup(func() { Processes = noProcesses })
}

func TestEveryLauncherKnowsItsOwnProcess(t *testing.T) {
	game := proc.Process{
		Exe:     "/Users/a/Library/Application Support/minecraft/runtime/java-runtime-delta/jre.bundle/Contents/Home/bin/java",
		Command: "java -cp /Applications/ATLauncher.app/Contents/Java/libraries/lwjgl.jar net.minecraft.client.main.Main --gameDir /Users/a/Library/Application Support/PrismLauncher/instances/smp/minecraft",
	}
	for _, tc := range []struct {
		launcher string
		open     proc.Process
	}{
		{"prism", proc.Process{Exe: "/Users/a/Applications/Prism Launcher.app/Contents/MacOS/prismlauncher"}},
		{"prism", proc.Process{Exe: "prismlauncher.exe"}},
		{"prism", proc.Process{Exe: "/nix/store/abc-prismlauncher-9.4/bin/.prismlauncher-wrapped"}},
		{"multimc", proc.Process{Exe: "/opt/multimc/bin/MultiMC"}},
		{"multimc", proc.Process{Exe: "MultiMC.exe"}},
		{"mojang", proc.Process{Exe: "/Applications/Minecraft.app/Contents/MacOS/launcher"}},
		{"mojang", proc.Process{Exe: "/usr/bin/minecraft-launcher"}},
		{"mojang", proc.Process{Exe: "MinecraftLauncher.exe"}},
		{"mojang", proc.Process{Exe: "Minecraft.exe"}},
		{"atlauncher", proc.Process{Exe: "/usr/bin/java", Command: "/usr/bin/java -cp /Applications/ATLauncher.app/Contents/Java/ATLauncher.jar com.atlauncher.App"}},
		{"atlauncher", proc.Process{Exe: "/usr/lib/jvm/java-21/bin/java", Command: "java -Dawt.useSystemAAFontSettings=on -Dswing.aatext=true -jar ATLauncher.jar --install-method=deb"}},
		{"gdlauncher", proc.Process{Exe: "GDLauncher.exe"}},
	} {
		e := Find(tc.launcher)
		withProcesses(t, game)
		if running, detectable := e.Process.running(); running || !detectable {
			t.Errorf("%s with only a game running: running %v, detectable %v", tc.launcher, running, detectable)
		}
		withProcesses(t, game, tc.open)
		if running, detectable := e.Process.running(); !running || !detectable {
			t.Errorf("%s with %s running: running %v, detectable %v", tc.launcher, tc.open.Exe, running, detectable)
		}
		for _, other := range All {
			if other.Process == nil || other.Name == tc.launcher {
				continue
			}
			if running, _ := other.Process.running(); running {
				t.Errorf("%s reads %s as its own", other.Name, tc.open.Exe)
			}
		}
	}
	for _, e := range All {
		if e != Shulker && e.Process == nil {
			t.Errorf("%s names no process", e.Name)
		}
	}
}

func TestAJavaProcessWithNoCommandLineLeavesAJarLauncherUnknown(t *testing.T) {
	withProcesses(t, proc.Process{Exe: "javaw.exe"})
	if running, detectable := atlauncherEntry.IsRunning(); running || detectable {
		t.Fatalf("ATLauncher: running %v, detectable %v", running, detectable)
	}
	if running, detectable := prismEntry.IsRunning(); running || !detectable {
		t.Fatalf("Prism: running %v, detectable %v", running, detectable)
	}
	withProcesses(t, proc.Process{Exe: "explorer.exe"})
	if running, detectable := atlauncherEntry.IsRunning(); running || !detectable {
		t.Fatalf("ATLauncher with no Java running: running %v, detectable %v", running, detectable)
	}
}

func TestIsRunningAsksTheProcessesWhereTheLockCantTell(t *testing.T) {
	e := &Entry{Process: &Process{Names: []string{"gdlauncher"}}, running: func() (bool, bool) { return false, false }}
	if _, detectable := e.IsRunning(); detectable {
		t.Fatal("no lock and no process list can't tell")
	}
	withProcesses(t, proc.Process{Exe: "GDLauncher.exe"})
	if running, detectable := e.IsRunning(); !running || !detectable {
		t.Fatalf("running %v, detectable %v", running, detectable)
	}
	e.running = func() (bool, bool) { return false, true }
	if running, detectable := e.IsRunning(); running || !detectable {
		t.Fatal("a lock that can tell is believed")
	}
}

func TestRestartNoteIsOnlyForALauncherThatMayBeOpen(t *testing.T) {
	if got := restartNote(prismEntry, "so the change is picked up"); got != "restart Prism Launcher if it is open so the change is picked up" {
		t.Fatalf("unknown: %q", got)
	}
	withProcesses(t)
	if got := restartNote(prismEntry, "so the change is picked up"); got != "" {
		t.Fatalf("closed: %q", got)
	}
	withProcesses(t, proc.Process{Exe: "/usr/bin/prismlauncher"})
	if got := restartNote(prismEntry, "so the change is picked up"); got != "restart Prism Launcher so the change is picked up" {
		t.Fatalf("open: %q", got)
	}
}
