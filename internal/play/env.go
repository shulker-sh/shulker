// Package play assembles a launch of a shulker instance: the sync before it, the store filled
// with what the lock names, the Java and settings it runs with, and who plays it. The CLI's play
// command is its caller, for a dry run and a real launch alike.
package play

import (
	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/sync"
)

// Env is what a launch reaches beyond a sync: the store it assembles the game in, the config.json
// whose play.* defaults and accounts it reads, the sign-in that renews a session, shulker's own
// version for the argv, and how it asks which account plays.
type Env struct {
	*sync.Env
	Store game.Store
	// Config is the path of config.json; accounts.json sits beside it.
	Config  string
	SignIn  *account.SignIn
	Version string
	// AskAccount is asked which account plays when there is no default and several could; nil
	// names the flag instead.
	AskAccount account.Picker
}
