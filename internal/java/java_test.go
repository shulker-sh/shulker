package java

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

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

func TestFindJavaTakesABinaryOrAHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake java is a shell script")
	}
	home := t.TempDir()
	bin := filepath.Join(home, "bin", "java")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'openjdk version \"1.8.0_74\"' >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{home, bin} {
		j, err := Find(override, 8)
		if err != nil || j.Path != bin || j.Major != 8 {
			t.Errorf("%s: got %+v, %v", override, j, err)
		}
	}
}
