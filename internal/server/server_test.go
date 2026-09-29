package server

import (
	"strings"
	"testing"
)

func TestJVMArgsPresets(t *testing.T) {
	small, err := JVMArgs("", "")
	if err != nil {
		t.Fatal(err)
	}
	if small[0] != "-Xms4G" || small[1] != "-Xmx4G" || !contains(small, "-XX:G1HeapRegionSize=8M") || contains(small, "-XX:G1HeapRegionSize=16M") {
		t.Fatalf("small: %v", small)
	}
	large, err := JVMArgs("12288M", "aikars")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(large, "-XX:G1HeapRegionSize=16M") {
		t.Fatalf("large: %v", large)
	}
	none, err := JVMArgs("2g", "none")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(none, " ") != "-Xms2g -Xmx2g" {
		t.Fatalf("none: %v", none)
	}
	for _, bad := range []struct{ memory, preset string }{{"4", ""}, {"0G", ""}, {"4G", "zgc"}} {
		if _, err := JVMArgs(bad.memory, bad.preset); err == nil {
			t.Fatalf("expected error for %+v", bad)
		}
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
