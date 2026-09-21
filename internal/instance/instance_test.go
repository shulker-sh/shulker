package instance

import (
	"testing"
)

// The schema closes `settings` to extra keys, so a setting the code reads but the schema omits
// makes a hand-edited file fail validation. Every setting round-trips here to catch that drift.
func TestSaveLoadKeepsEverySetting(t *testing.T) {
	dir := t.TempDir()
	keep := -1
	f := New()
	f.Source, f.Ref, f.Side = "/packs/smp", "main", "client"
	f.Settings.Commands = &Commands{PreLaunch: "echo before", PostExit: "echo after"}
	f.Settings.Java = "/opt/java/bin/java"
	f.Settings.Wrapper = []string{"gamemoderun"}
	f.Settings.Memory = "6G"
	f.Settings.JVMArgs = []string{"-XX:+UseZGC"}
	f.Settings.Window = "1280x720"
	f.Settings.Account = "069a79f444e94726a5befca90e38aaf5"
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
	if got.Settings.Memory != "6G" || got.Settings.Window != "1280x720" || got.Settings.Account != f.Settings.Account {
		t.Errorf("memory = %q, window = %q, account = %q", got.Settings.Memory, got.Settings.Window, got.Settings.Account)
	}
	if len(got.Settings.JVMArgs) != 1 || got.Settings.JVMArgs[0] != "-XX:+UseZGC" {
		t.Errorf("jvmArgs = %q", got.Settings.JVMArgs)
	}
	if got.Settings.Commands == nil || got.Settings.Commands.PreLaunch != "echo before" {
		t.Errorf("commands = %+v", got.Settings.Commands)
	}
}

func TestJavaPrefersTheSettingOverTheResolvedOne(t *testing.T) {
	f := New()
	if got := f.Java(); got != "" {
		t.Fatalf("no Java recorded: got %q", got)
	}
	f.Resolved = &Resolved{Java: "/cache/java/bin/java"}
	if got := f.Java(); got != "/cache/java/bin/java" {
		t.Fatalf("resolved only: got %q", got)
	}
	f.Settings.Java = "/opt/java/bin/java"
	if got := f.Java(); got != "/opt/java/bin/java" {
		t.Fatalf("setting and resolved: got %q", got)
	}
}

func TestEnsureResolvedKeepsWhatIsRecorded(t *testing.T) {
	f := New()
	f.EnsureResolved().LastResult = ResultOK
	if f.Resolved == nil || f.Resolved.LastResult != ResultOK {
		t.Fatalf("resolved not created: %+v", f.Resolved)
	}
	f.EnsureResolved().Java = "/cache/java/bin/java"
	if f.Resolved.LastResult != ResultOK {
		t.Fatalf("existing resolved replaced: %+v", f.Resolved)
	}
}
