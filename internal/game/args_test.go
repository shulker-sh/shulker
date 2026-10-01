package game

import (
	"slices"
	"testing"
)

func TestWithLaunchSettingsPutsJVMOptionsBeforeTheMainClass(t *testing.T) {
	argv := []string{"-Xmx2G", "-XX:+UseG1GC", "--add-modules", "ALL-MODULE-PATH", "-cp", "a.jar:b.jar", "net.minecraft.client.main.Main", "--gameDir", "/g", "--width", "854", "--height", "480"}
	got := WithLaunchSettings(argv, "6G", []string{"-XX:+UseZGC"}, "1280x720")
	want := []string{"-Xmx2G", "-XX:+UseG1GC", "--add-modules", "ALL-MODULE-PATH", "-cp", "a.jar:b.jar", "-Xms6G", "-Xmx6G", "-XX:+UseZGC", "net.minecraft.client.main.Main", "--gameDir", "/g", "--width", "1280", "--height", "720"}
	if !slices.Equal(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if argv[0] != "-Xmx2G" || argv[10] != "854" {
		t.Fatalf("the argv given was changed: %q", argv)
	}
	if got := WithLaunchSettings(argv, "", nil, ""); !slices.Equal(got, argv) {
		t.Fatalf("nothing set changes nothing: %q", got)
	}
	sized := WithLaunchSettings([]string{"-cp", "a.jar", "Main", "--gameDir", "/g"}, "", nil, "1280x720")
	if want := []string{"-cp", "a.jar", "Main", "--gameDir", "/g", "--width", "1280", "--height", "720"}; !slices.Equal(sized, want) {
		t.Fatalf("a size the launcher gave none of is added: %q", sized)
	}
	if got := WithLaunchSettings([]string{"-version"}, "6G", nil, "1280x720"); !slices.Equal(got, []string{"-version"}) {
		t.Fatalf("an argv with no main class comes back as it was: %q", got)
	}
}

func TestALaunchRunsBehindItsWrapperThenItsSandbox(t *testing.T) {
	l := Launch{Java: "/java", Argv: []string{"-cp", "a.jar", "Main"}, Wrapper: []string{"gamemoderun"}, Sandbox: []string{"/shulker", "hook", "sandbox", "--"}}
	if want := []string{"gamemoderun", "/shulker", "hook", "sandbox", "--", "/java", "-cp", "a.jar", "Main"}; !slices.Equal(l.command(l.Wrapper), want) {
		t.Fatalf("got %q", l.command(l.Wrapper))
	}
	if want := []string{"/shulker", "hook", "sandbox", "--", "/java", "-cp", "a.jar", "Main"}; !slices.Equal(l.command(nil), want) {
		t.Fatalf("a wrapper that can't run gives way, the sandbox never does: %q", l.command(nil))
	}
	if l.Program() != "gamemoderun" || (Launch{Java: "/java"}).Program() != "/java" || (Launch{Java: "/java", Sandbox: l.Sandbox}).Program() != "/shulker" {
		t.Fatal("Program is the first word of the command")
	}
}
