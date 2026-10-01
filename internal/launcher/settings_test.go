package launcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/instance"
)

func settingsFile() *instance.File {
	f := instance.New()
	f.Settings.Memory = "6G"
	f.Settings.JVMArgs = []string{"-XX:+UseZGC", "-Dpath=/my games/x"}
	f.Settings.Window = "1280x720"
	return f
}

func TestReconcileWritesTheInstancesLaunchSettingsIntoPrismAndMultiMC(t *testing.T) {
	for name, file := range map[string]string{"prism": PrismInstanceFile, "multimc": MultiMCInstanceFile} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := filepath.Join(dir, file)
			write(t, cfg, "[General]\nname=Cozy\nMaxMemAlloc=2048\nJvmArgs=-Dold=1\n")
			e, unescape := Find(name), prismUnescape
			if name == "multimc" {
				unescape = multimcUnescape
			}
			if _, err := Reconcile(e, slotRow(name, dir), instance.New(), "/bin/shulker", false); err != nil {
				t.Fatal(err)
			}
			values, err := readINI(cfg, unescape)
			if err != nil {
				t.Fatal(err)
			}
			if _, has := values["OverrideMemory"]; has || values["MaxMemAlloc"] != "2048" || values["JvmArgs"] != "-Dold=1" {
				t.Fatalf("an instance that sets nothing leaves the launcher's settings alone: %+v", values)
			}

			r, err := Reconcile(e, slotRow(name, dir), settingsFile(), "/bin/shulker", false)
			if err != nil || len(r.Unapplied) != 0 {
				t.Fatalf("reconcile: %+v %v", r, err)
			}
			if values, err = readINI(cfg, unescape); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]string{
				"OverrideMemory": "true", "MinMemAlloc": "6144", "MaxMemAlloc": "6144",
				"OverrideJavaArgs": "true", "JvmArgs": `-XX:+UseZGC "-Dpath=/my games/x"`,
				"OverrideWindow": "true", "LaunchMaximized": "false", "MinecraftWinWidth": "1280", "MinecraftWinHeight": "720",
				"name": "Cozy",
			} {
				if values[key] != want {
					t.Errorf("%s = %q, want %q", key, values[key], want)
				}
			}
		})
	}
}

func TestReconcileWritesMemoryAndArgumentsIntoATLauncherButNoWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ATLauncherInstanceFile)
	write(t, path, `{"launcher":{"name":"Cozy","maximumMemory":2048}}`)
	f := settingsFile()
	f.Settings.JVMArgs = []string{"-XX:+UseZGC", "-Dx=1"}
	r, err := Reconcile(Find("atlauncher"), slotRow("atlauncher", dir), f, "/bin/shulker", false)
	if err != nil || !slices.Equal(r.Unapplied, []string{"window"}) {
		t.Fatalf("ATLauncher has no window size of an instance's own: %+v %v", r, err)
	}
	var got struct {
		Launcher struct {
			Name          string `json:"name"`
			MaximumMemory int    `json:"maximumMemory"`
			JavaArguments string `json:"javaArguments"`
		} `json:"launcher"`
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &got) != nil {
		t.Fatal(err)
	}
	if got.Launcher.MaximumMemory != 6144 || got.Launcher.JavaArguments != "-XX:+UseZGC -Dx=1" || got.Launcher.Name != "Cozy" {
		t.Fatalf("instance.json: %s", data)
	}
}

func TestReconcileWritesLaunchSettingsIntoGDLaunchersGameConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, GDLauncherInstanceFile)
	write(t, path, `{"name":"Cozy","game_configuration":{"version":{"release":"26.2"},"global_java_args":true}}`)
	if _, err := Reconcile(Find("gdlauncher"), slotRow("gdlauncher", dir), instance.New(), "/bin/shulker", false); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Game struct {
			Version struct {
				Release string `json:"release"`
			} `json:"version"`
			GlobalJavaArgs bool `json:"global_java_args"`
			Memory         *struct {
				Min int `json:"min_mb"`
				Max int `json:"max_mb"`
			} `json:"memory"`
			ExtraJavaArgs  string `json:"extra_java_args"`
			GameResolution string `json:"game_resolution"`
		} `json:"game_configuration"`
	}
	read := func() {
		t.Helper()
		got.Game.Memory = nil
		data, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(data, &got) != nil {
			t.Fatalf("%s: %v", data, err)
		}
	}
	if read(); got.Game.Memory != nil || got.Game.ExtraJavaArgs != "" || got.Game.GameResolution != "" {
		t.Fatalf("an instance that sets nothing leaves the launcher's settings alone: %+v", got.Game)
	}
	if _, err := Reconcile(Find("gdlauncher"), slotRow("gdlauncher", dir), settingsFile(), "/bin/shulker", false); err != nil {
		t.Fatal(err)
	}
	read()
	if got.Game.Memory == nil || got.Game.Memory.Min != 6144 || got.Game.Memory.Max != 6144 || got.Game.GameResolution != "custom:1280x720" {
		t.Fatalf("game_configuration: %+v", got.Game)
	}
	if got.Game.ExtraJavaArgs != `-XX:+UseZGC "-Dpath=/my games/x"` || !got.Game.GlobalJavaArgs || got.Game.Version.Release != "26.2" {
		t.Fatalf("game_configuration: %+v", got.Game)
	}
}
