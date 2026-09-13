package curseforge

import "testing"

func TestFingerprint(t *testing.T) {
	for _, c := range []struct {
		data string
		want uint32
	}{
		{"", 1540447798},
		{"a", 626045324},
		{"ab", 1692487918},
		{"abc", 1621425345},
		{"abcd", 3376380438},
		{"shulker", 3817166195},
		{"shu lker\r\n\t", 3817166195},
	} {
		if got := Fingerprint([]byte(c.data)); got != c.want {
			t.Errorf("Fingerprint(%q) = %d, want %d", c.data, got, c.want)
		}
	}
}
