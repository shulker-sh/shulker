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
