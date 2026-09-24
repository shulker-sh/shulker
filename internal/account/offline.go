package account

import (
	"crypto/md5"
	"fmt"
)

// OfflineUUID is the UUID an offline-mode host derives for a name: Java's type 3 UUID over
// "OfflinePlayer:<name>", which is what UUIDUtil.createOfflineProfile answers on a dedicated
// server and in a LAN world a mod has opened. The name is hashed as it was typed, so two
// spellings are two players.
func OfflineUUID(name string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	sum[6] = sum[6]&0x0f | 0x30
	sum[8] = sum[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// NewOffline is an account shulker created itself. It holds no token: its profile is the name and
// the UUID the client plays under, and nothing else.
func NewOffline(name, id string) Account {
	return Account{Type: Offline, Profile: &Profile{ID: id, Name: name}}
}

// OwnsTheGame reports whether shulker can see an account that owns Java Edition: the gate an
// offline account passes at creation, and again at deletion, since the gate would block creating
// it a second time. It is a statement of intent rather than a licence check, so it reads the Java
// profile an account already carries and asks nothing of anybody. A profile is the proof whether
// or not its session still works: an expired sign-in or borrowed token changes who can launch, not
// who owns the game.
func OwnsTheGame(accounts []Resolved) bool {
	for _, r := range accounts {
		switch r.State {
		case Playable, SignInExpired, TokenExpired:
			return true
		}
	}
	return false
}
