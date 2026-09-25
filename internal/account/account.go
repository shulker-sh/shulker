// Package account holds the accounts shulker can launch with: the ones it signed in or created
// itself, kept in accounts.json, and the ones it reads from another launcher's own file.
package account

import (
	"time"

	"shulker.sh/shulker/internal/out"
)

// Kind is what an account is, in shulker's own file.
type Kind string

const (
	Microsoft Kind = "microsoft"
	Offline   Kind = "offline"
)

// Account is one entry of accounts.json. A Microsoft account's Java profile is also the proof it
// owns Java Edition, so there is no separate ownership field; one without a profile is named by
// its gamertag and identified by its Xbox user id, since it has no player UUID.
type Account struct {
	Type         Kind       `json:"type"`
	Profile      *Profile   `json:"profile,omitempty"`
	Xbox         *Xbox      `json:"xbox,omitempty"`
	RefreshToken string     `json:"refreshToken,omitempty"`
	Minecraft    *Minecraft `json:"minecraft,omitempty"`
}

// Profile is an account's Java profile: its player UUID and username.
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Xbox is who an account is on Xbox Live, and the user token a renewal starts from while it holds.
type Xbox struct {
	XUID     string `json:"xuid,omitempty"`
	Gamertag string `json:"gamertag,omitempty"`
	UserHash string `json:"userHash,omitempty"`
	Token    string `json:"token,omitempty"`
	NotAfter string `json:"notAfter,omitempty"`
}

// Minecraft is the session token a launch hands the game.
type Minecraft struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

// ID is what names the account everywhere shulker prints or takes one: the player UUID when it
// owns a Java profile, and the Xbox user id when it doesn't.
func (a Account) ID() string {
	if a.Profile != nil && a.Profile.ID != "" {
		return a.Profile.ID
	}
	if a.Xbox != nil {
		return a.Xbox.XUID
	}
	return ""
}

// Name is the Minecraft username, or the gamertag when the account owns no Java profile.
func (a Account) Name() string {
	if a.Profile != nil && a.Profile.Name != "" {
		return a.Profile.Name
	}
	if a.Xbox != nil {
		return a.Xbox.Gamertag
	}
	return ""
}

// State is what shulker can do with an account.
type State string

const (
	Playable      State = "playable"
	NoProfile     State = "no-profile"
	SignInExpired State = "sign-in-expired"
	TokenExpired  State = "token-expired"
	OfflineOnly   State = "offline"
)

// State reads an own or offline account's state off the record. A launcher account's token-expired
// state comes from its provider's reader, which knows when the token ran out.
func (a Account) State() State {
	switch {
	case a.Type == Offline:
		return OfflineOnly
	case a.Profile == nil:
		return NoProfile
	case a.RefreshToken == "":
		return SignInExpired
	}
	return Playable
}

// Text is the state as the account list prints it. An expired launcher token says how long
// ago, which is the only thing a player can act on: the other launcher has to renew it.
func (s State) Text(expired, now time.Time) string {
	switch s {
	case Playable:
		return "playable"
	case NoProfile:
		return "not playable (no Java profile)"
	case SignInExpired:
		return "sign-in expired"
	case TokenExpired:
		if expired.IsZero() {
			return "token expired"
		}
		return "token expired " + out.Since(now.Sub(expired))
	}
	return "offline"
}
