package account

import (
	"crypto/md5"
	"fmt"
	"strings"

	"shulker.sh/shulker/internal/out"
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
// or not its session still works: an expired sign-in or launcher token changes who can launch, not
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

// FreeToCreate is what an offline account may not be: an id another account already has, which is
// what shulker's own file is keyed by and so is refused however hard a run insists, or, without
// force, a name someone else already answers to.
func FreeToCreate(accounts []Resolved, store Store, name, id string, force bool) error {
	if have, taken := playsUnder(accounts, store, id); taken {
		e := out.Errorf("account-exists", "%s already plays under %s, and two accounts can't share a uuid", have, id)
		e.Nudge = out.Nudge{Lead: "Give the new account its own uuid", Command: "shulker accounts add " + QuoteName(name) + " --force --uuid <uuid>"}
		return e
	}
	if force {
		return nil
	}
	for _, r := range accounts {
		if !strings.EqualFold(r.Name, name) {
			continue
		}
		e := out.Errorf("account-exists", "%s is already %s", name, whereItCameFrom(r))
		e.Rows = []out.Detail{{Label: "Have", Text: r.Qualifier() + " — " + r.ID}}
		e.Nudge = out.Nudge{Lead: "Create a second account with that name", Command: "shulker accounts add " + QuoteName(name) + " --force --uuid <uuid>"}
		return e
	}
	return nil
}

// playsUnder names whoever already has an id. Shulker's own file is searched beside the accounts
// the providers yield, because storing an account is keyed by id: one the configured providers
// don't read back would be replaced rather than added.
func playsUnder(accounts []Resolved, store Store, id string) (string, bool) {
	for _, r := range accounts {
		if SameID(r.ID, id) {
			return r.Name, true
		}
	}
	for _, have := range store.Accounts {
		if SameID(have.ID(), id) {
			return have.Name(), true
		}
	}
	return "", false
}

// whereItCameFrom names an account the way a clash has to explain it: what is already there is
// either a sign-in, an offline account or another launcher's.
func whereItCameFrom(r Resolved) string {
	switch r.Source {
	case SourceShulker:
		return "signed in"
	case SourceOffline:
		return "an offline account"
	}
	return "an account from " + r.Source
}

// QuoteName quotes a name a shell would otherwise split: a gamertag may hold spaces, and so may an
// offline name created with --allow-invalid-name.
func QuoteName(name string) string {
	if strings.ContainsAny(name, " \t") {
		return `"` + name + `"`
	}
	return name
}
