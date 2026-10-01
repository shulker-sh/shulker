package launcher

import (
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/instance"
)

func prismWrapper(t *testing.T, dir string) string {
	t.Helper()
	values, err := readINI(filepath.Join(dir, PrismInstanceFile), prismUnescape)
	if err != nil {
		t.Fatal(err)
	}
	return values["WrapperCommand"]
}

func TestReconcileOwnsTheWrapperSlotWhileTheSandboxIsOn(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, PrismInstanceFile), "[General]\nname=Cozy\nWrapperCommand=mangohud --dlsym\n")
	e, in := Find("prism"), slotRow("prism", dir)
	f := instance.New()

	r, err := Reconcile(e, in, f, "/opt/shulker", true)
	if err != nil {
		t.Fatal(err)
	}
	if r.AdoptedWrapper != "mangohud --dlsym" || !slices.Equal(f.Settings.Wrapper, []string{"mangohud", "--dlsym"}) {
		t.Fatalf("the player's wrapper moves into the instance's settings: %+v %v", r, f.Settings.Wrapper)
	}
	if got := prismWrapper(t, dir); got != "mangohud --dlsym /opt/shulker hook sandbox --" || !IsSandboxWrapper(got) {
		t.Fatalf("the sandbox goes innermost, after the player's wrapper: %q", got)
	}

	if r, err = Reconcile(e, in, f, "/opt/shulker", true); err != nil || r.AdoptedWrapper != "" {
		t.Fatalf("a second reconcile adopts nothing: %+v %v", r, err)
	}
	if got := prismWrapper(t, dir); got != "mangohud --dlsym /opt/shulker hook sandbox --" {
		t.Fatalf("slot after a second reconcile: %q", got)
	}

	if _, err = Reconcile(e, in, f, "/opt/shulker", false); err != nil {
		t.Fatal(err)
	}
	if got := prismWrapper(t, dir); got != "mangohud --dlsym" {
		t.Fatalf("with the sandbox off the slot is the player's wrapper again: %q", got)
	}

	f.Settings.Wrapper = nil
	if err := WriteSlots(e, in, Slots{ClearWrapper: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = Reconcile(e, in, f, "/opt/shulker", true); err != nil {
		t.Fatal(err)
	}
	if got := prismWrapper(t, dir); got != "/opt/shulker hook sandbox --" {
		t.Fatalf("no wrapper of the player's: %q", got)
	}
	if _, err = Reconcile(e, in, f, "/opt/shulker", false); err != nil {
		t.Fatal(err)
	}
	if got := prismWrapper(t, dir); got != "" {
		t.Fatalf("a sandbox with no wrapper behind it is cleared, not left: %q", got)
	}
}

func TestReleaseSlotsTakesTheSandboxOutOfTheWrapperSlot(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, PrismInstanceFile), "[General]\nname=Cozy\n")
	e, in := Find("prism"), slotRow("prism", dir)
	f := instance.New()
	f.Settings.Wrapper = []string{"gamemoderun"}
	if _, err := Reconcile(e, in, f, "/opt/shulker", true); err != nil {
		t.Fatal(err)
	}
	if got := prismWrapper(t, dir); got != "gamemoderun /opt/shulker hook sandbox --" {
		t.Fatalf("slot: %q", got)
	}
	if _, _, err := ReleaseSlots(e, in); err != nil {
		t.Fatal(err)
	}
	if got := prismWrapper(t, dir); got != "gamemoderun" {
		t.Fatalf("an unlinked instance must start without shulker: %q", got)
	}
}

func TestATLauncherCantCarryASandboxAtAPathWithASpace(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ATLauncherInstanceFile), `{"launcher":{"name":"Cozy"}}`)
	r, err := Reconcile(Find("atlauncher"), slotRow("atlauncher", dir), instance.New(), "/Users/me/My Tools/shulker", true)
	if err != nil || !slices.Equal(r.Unapplied, []string{"sandbox"}) {
		t.Fatalf("ATLauncher splits its wrapper on spaces: %+v %v", r, err)
	}
	slots, _, err := ReadSlots(Find("atlauncher"), slotRow("atlauncher", dir))
	if err != nil || slots.Wrapper != "" {
		t.Fatalf("no half-written sandbox: %+v %v", slots, err)
	}
	if r, err = Reconcile(Find("atlauncher"), slotRow("atlauncher", dir), instance.New(), "/opt/shulker", true); err != nil || len(r.Unapplied) != 0 {
		t.Fatalf("a path with no space is fine: %+v %v", r, err)
	}
	if slots, _, _ = ReadSlots(Find("atlauncher"), slotRow("atlauncher", dir)); slots.Wrapper != "/opt/shulker hook sandbox --" {
		t.Fatalf("slot: %q", slots.Wrapper)
	}
}

func TestAnAdoptedWrapperIsSplitTheWayItsLauncherSplitsIt(t *testing.T) {
	quotes, bare := *Find("prism").Slot, *Find("atlauncher").Slot
	for command, want := range map[string][]string{
		`gamemoderun`:                                {"gamemoderun"},
		`  mangohud   --dlsym `:                      {"mangohud", "--dlsym"},
		`"/opt/my tools/wrap" --flag`:                {"/opt/my tools/wrap", "--flag"},
		`env 'A=b c' "D=\"e\"" -javaagent:"x y".jar`: {"env", "A=b c", `D="e"`, "-javaagent:x y.jar"},
		`C:\tools\wrap.exe ""`:                       {`C:\tools\wrap.exe`, ""},
	} {
		if got := splitWrapper(quotes, command); !slices.Equal(got, want) {
			t.Errorf("splitWrapper(%q) = %q, want %q", command, got, want)
		}
	}
	if got := splitWrapper(bare, `"/opt/my tools/wrap" --flag`); !slices.Equal(got, []string{`"/opt/my`, `tools/wrap"`, "--flag"}) {
		t.Errorf("a launcher that ignores quotes splits on spaces alone: %q", got)
	}

	dir := t.TempDir()
	write(t, filepath.Join(dir, PrismInstanceFile), "[General]\nname=Cozy\nWrapperCommand=\"\\\"/opt/my tools/wrap\\\" --flag\"\n")
	f := instance.New()
	if _, err := Reconcile(Find("prism"), slotRow("prism", dir), f, "/opt/shulker", true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.Settings.Wrapper, []string{"/opt/my tools/wrap", "--flag"}) {
		t.Fatalf("adopted words: %q", f.Settings.Wrapper)
	}
	if got := prismWrapper(t, dir); got != `"/opt/my tools/wrap" --flag /opt/shulker hook sandbox --` {
		t.Fatalf("the slot keeps the wrapper's path whole: %q", got)
	}
}
