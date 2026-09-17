package instance

import (
	"testing"
)

// The schema closes `settings` to extra keys, so a setting the code reads but the schema omits
// makes a hand-edited file fail validation. Every setting round-trips here to catch that drift.
func TestSaveLoadKeepsEverySetting(t *testing.T) {
	dir := t.TempDir()
	keep := -1
	f := New("/packs/smp", "main", "client")
	f.Settings.Commands = &Commands{PreLaunch: "echo before", PostExit: "echo after"}
	f.Settings.Java = "/opt/java/bin/java"
	f.Settings.Wrapper = []string{"gamemoderun"}
	f.Settings.Shulker = "/usr/local/bin/shulker"
	f.Settings.LaunchHistory = &keep
	if err := f.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Settings.LaunchKeep() != keep {
		t.Errorf("launchHistory = %d, want %d", got.Settings.LaunchKeep(), keep)
	}
	if got.Settings.Java != f.Settings.Java || got.Settings.Shulker != f.Settings.Shulker {
		t.Errorf("java = %q, shulker = %q", got.Settings.Java, got.Settings.Shulker)
	}
	if len(got.Settings.Wrapper) != 1 || got.Settings.Wrapper[0] != "gamemoderun" {
		t.Errorf("wrapper = %q", got.Settings.Wrapper)
	}
	if got.Settings.Commands == nil || got.Settings.Commands.PreLaunch != "echo before" {
		t.Errorf("commands = %+v", got.Settings.Commands)
	}
}
