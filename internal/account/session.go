package account

import (
	"context"
	"time"

	"shulker.sh/shulker/internal/fetch"
)

// Warning is why a session plays on a token online servers may reject, for the caller to say.
type Warning string

const (
	// WarnNone is a session with nothing to say.
	WarnNone Warning = ""
	// WarnTokenExpired is a launcher account's session token that ran out, which only its launcher renews.
	WarnTokenExpired Warning = "token-expired"
	// WarnOffline is an own account that couldn't be renewed because Microsoft was unreachable.
	WarnOffline Warning = "offline"
)

// Session is the account a launch plays on: the stored one while its Minecraft token has over an
// hour left at now, and a silently renewed one otherwise, which renewed reports so the caller can
// save it. A launcher account is never renewed: an expired one plays on the token it has, with a
// warning. Offline, an own account falls back to the token it has too, which still opens
// singleplayer, LAN and offline-mode servers.
func (s *SignIn) Session(ctx context.Context, r Resolved, now time.Time) (Account, bool, Warning, error) {
	if err := r.State.Error(r.Name); err != nil {
		return Account{}, false, WarnNone, err
	}
	if r.State == TokenExpired {
		return r.Account, false, WarnTokenExpired, nil
	}
	if r.Group != GroupOwn || r.Account.IsFresh(now) {
		return r.Account, false, WarnNone, nil
	}
	signed, err := s.Renew(ctx, r.Account)
	if err != nil {
		if fetch.IsNetwork(err) && r.Account.Minecraft != nil {
			return r.Account, false, WarnOffline, nil
		}
		return Account{}, false, WarnNone, err
	}
	return signed, true, WarnNone, nil
}
