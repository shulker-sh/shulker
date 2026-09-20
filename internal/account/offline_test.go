package account

import (
	"strings"
	"testing"
)

func TestOfflineUUIDIsWhatAHostDerives(t *testing.T) {
	// What Java's UUID.nameUUIDFromBytes("OfflinePlayer:<name>") answers, which is the player an
	// offline-mode host keys by that name.
	for name, want := range map[string]string{
		"Steve":      "5627dd98-e6be-3c21-b8a8-e92344183641",
		"Notch":      "b50ad385-829d-3141-a216-7e7d7539ba7f",
		"jeb_":       "a762f560-4fce-3236-812a-b80efff0b62b",
		"Dinnerbone": "4d258a81-2358-3084-8166-05b9faccad80",
	} {
		if got := OfflineUUID(name); got != want {
			t.Errorf("OfflineUUID(%q) = %s, want %s", name, got, want)
		}
	}
}

func TestOfflineUUIDIsTypeThreeAndCaseSensitive(t *testing.T) {
	id := OfflineUUID("Steve")
	if id[14] != '3' {
		t.Errorf("%s is not a type 3 uuid", id)
	}
	if !strings.ContainsRune("89ab", rune(id[19])) {
		t.Errorf("%s does not carry the rfc 4122 variant", id)
	}
	if OfflineUUID("steve") == id {
		t.Error("a host reads the name as it was typed, so a differently cased one is another player")
	}
}
