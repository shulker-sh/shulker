package cli

const assumeClientWarning = "shulker.json declares no client; building from shared mods and overrides"

// warnAssumedClient says a client is being built from the shared mods and overrides, when a side
// choice assumed one.
func (a *app) warnAssumedClient(assumed bool) {
	if assumed {
		a.printer.Warn(assumeClientWarning)
	}
}

// firstArg is the one positional argument a command takes, or empty when none was given.
func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
