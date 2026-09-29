package cli

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"shulker.sh/shulker/internal/out"
)

// flagError rewords the errors pflag returns while parsing flags.
func flagError(err error) (*out.Error, bool) {
	var notExist *pflag.NotExistError
	var needsValue *pflag.ValueRequiredError
	var invalid *pflag.InvalidValueError
	var syntax *pflag.InvalidSyntaxError
	switch {
	case errors.As(err, &notExist):
		return out.Errorf("usage", "unknown flag %s", typedFlag(notExist.GetSpecifiedName(), notExist.GetSpecifiedShortnames())), true
	case errors.As(err, &needsValue):
		return out.Errorf("usage", "%s needs a value", typedFlag(needsValue.GetSpecifiedName(), needsValue.GetSpecifiedShortnames())), true
	case errors.As(err, &invalid):
		var reason flagReason
		if errors.As(err, &reason) {
			return out.Errorf("usage", "--%s %s", invalid.GetFlag().Name, string(reason)), true
		}
		if f := invalid.GetFlag(); f.Value.Type() == "bool" {
			return out.Errorf("usage", "--%s takes true or false, not %q", f.Name, invalid.GetValue()), true
		}
		return out.Errorf("usage", "--%s can't take %q", invalid.GetFlag().Name, invalid.GetValue()), true
	case errors.As(err, &syntax):
		return out.Errorf("usage", "can't read %q as a flag", syntax.GetSpecifiedFlag()), true
	}
	return nil, false
}

// flagReason is why a flag's value refuses what it was given, worded to follow the flag's name.
type flagReason string

func (r flagReason) Error() string { return string(r) }

// typedFlag is the flag as it was typed: pflag names a shorthand by its letter
// and says it came from a group like -xz.
func typedFlag(name, shorthands string) string {
	if shorthands != "" && name != "" {
		return "-" + string([]rune(name)[:1])
	}
	return "--" + name
}

var usePlaceholder = regexp.MustCompile(`<[^>]+>|\[[^\]]+\]`)

func noArgs(cmd *cobra.Command, args []string) error { return checkArgs(cmd, args, 0, 0) }

func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error { return checkArgs(cmd, args, n, n) }
}

func maximumArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error { return checkArgs(cmd, args, 0, n) }
}

func minimumArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error { return checkArgs(cmd, args, n, -1) }
}

func rangeArgs(least, most int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error { return checkArgs(cmd, args, least, most) }
}

// checkArgs lists what is missing by the placeholders in the command's usage
// line, or what is extra as typed. A most of -1 means no upper bound.
func checkArgs(cmd *cobra.Command, args []string, least, most int) error {
	if len(args) < least {
		placeholders := usePlaceholder.FindAllString(cmd.Use, -1)
		var missing []string
		for i := len(args); i < least; i++ {
			switch {
			case i < len(placeholders):
				missing = append(missing, placeholders[i])
			case len(placeholders) > 0:
				missing = append(missing, placeholders[len(placeholders)-1])
			default:
				missing = append(missing, fmt.Sprintf("argument %d", i+1))
			}
		}
		message := "missing an argument"
		switch n := len(missing); {
		case most < 0 && n == 1:
			message = "missing at least one argument"
		case most < 0:
			message = fmt.Sprintf("missing at least %d arguments", n)
		case n > 1:
			message = fmt.Sprintf("missing %d arguments", n)
		}
		e := out.Errorf("usage", "%s", message)
		e.Items = missing
		return e
	}
	if most >= 0 && len(args) > most {
		message := "unexpected argument"
		if len(args)-most > 1 {
			message = "unexpected arguments"
		}
		e := out.Errorf("usage", "%s", message)
		e.Items = args[most:]
		return e
	}
	return nil
}
