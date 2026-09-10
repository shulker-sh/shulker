package server

import (
	"strings"
	"testing"
)

func TestJVMArgsPresets(t *testing.T) {
	small, err := JVMArgs("", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if small[0] != "-Xms4G" || small[1] != "-Xmx4G" || !contains(small, "-XX:G1HeapRegionSize=8M") || contains(small, "-XX:G1HeapRegionSize=16M") {
		t.Fatalf("small: %v", small)
	}
	large, err := JVMArgs("12288M", "aikars", []string{"-Dextra=1"})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(large, "-XX:G1HeapRegionSize=16M") || large[len(large)-1] != "-Dextra=1" {
		t.Fatalf("large: %v", large)
	}
	none, err := JVMArgs("2g", "none", []string{"-XX:+UseZGC"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(none, " ") != "-Xms2g -Xmx2g -XX:+UseZGC" {
		t.Fatalf("none: %v", none)
	}
	for _, bad := range []struct{ memory, preset string }{{"4", ""}, {"0G", ""}, {"4G", "zgc"}} {
		if _, err := JVMArgs(bad.memory, bad.preset, nil); err == nil {
			t.Fatalf("expected error for %+v", bad)
		}
	}
}

func TestParseJavaMajor(t *testing.T) {
	cases := map[string]int{
		`openjdk version "25.0.1" 2025-10-21`:                     25,
		`java version "1.8.0_292"`:                                8,
		`openjdk version "17.0.12" 2024-07-16 LTS`:                17,
		`openjdk version "26-ea" 2026-03-17`:                      26,
		"Picked up JAVA_TOOL_OPTIONS\nopenjdk version \"21.0.4\"": 21,
	}
	for in, want := range cases {
		got, err := parseMajor([]byte(in))
		if err != nil || got != want {
			t.Errorf("%q: got %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := parseMajor([]byte("nothing here")); err == nil {
		t.Error("expected error for missing version")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
