package game

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func thinMacho(cpu macho.Cpu) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, []uint32{macho.Magic64, uint32(cpu), 3, uint32(macho.TypeExec), 0, 0, 0, 0})
	return b.Bytes()
}

func fatMacho(cpus ...macho.Cpu) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, []uint32{macho.MagicFat, uint32(len(cpus))})
	offset := uint32(4096)
	for _, cpu := range cpus {
		binary.Write(&b, binary.BigEndian, []uint32{uint32(cpu), 3, offset, 32, 12})
		offset += 4096
	}
	for _, cpu := range cpus {
		b.Write(make([]byte, 4096-b.Len()%4096))
		b.Write(thinMacho(cpu))
	}
	return b.Bytes()
}

func TestMachoArch(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, host, want string
		data             []byte
		ok               bool
	}{
		{"intel", "arm64", "x86_64", thinMacho(macho.CpuAmd64), true},
		{"arm", "arm64", "arm64", thinMacho(macho.CpuArm64), true},
		{"universal", "arm64", "arm64", fatMacho(macho.CpuAmd64, macho.CpuArm64), true},
		{"universal-intel-host", "x86_64", "x86_64", fatMacho(macho.CpuAmd64, macho.CpuArm64), true},
		{"script", "arm64", "", []byte("#!/bin/sh\n"), false},
	}
	for _, c := range cases {
		path := filepath.Join(dir, c.name)
		if err := os.WriteFile(path, c.data, 0o755); err != nil {
			t.Fatal(err)
		}
		if got, ok := machoArch(path, c.host); got != c.want || ok != c.ok {
			t.Errorf("%s: got %q, %v; want %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}
