package cli

import (
	"slices"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

var sideNames = []string{"client", "server"}

func isSide(name string) bool { return name == "client" || name == "server" }

// checkSide reads a side out of a positional argument or a --side flag.
func checkSide(name, flag string) error {
	if isSide(name) {
		return nil
	}
	e := out.Errorf("usage", "%q is not a side", name)
	e.Candidates, e.Given, e.Flag = sideNames, name, flag
	return e
}

const assumeClientWarning = "shulker.json declares no client; building from shared mods and overrides"

// noSide is what a local-project command raises when the manifest declares no
// block for the side it needs; its user can add the block.
func noSide(side string) error {
	e := out.Errorf("no-side", "%s declares no %s", manifest.FileName, side)
	e.Rows = []out.Detail{{Label: "Fix", Text: `add "` + side + `": {} to ` + manifest.FileName}}
	return e
}

// ambiguousSide is what a command that works on one side raises when the
// manifest declares both and nothing chose between them. flag names the flag
// that would, or is empty when the side is positional.
func ambiguousSide(sides []string, flag string) error {
	e := out.Errorf("ambiguous-side", "%s declares both sides; choose one", manifest.FileName)
	e.Candidates, e.Flag = sides, flag
	return e
}

// declaredSide is a side a flag or argument names, which the manifest must
// declare.
func declaredSide(m *manifest.Manifest, name, flag string) (string, error) {
	if err := checkSide(name, flag); err != nil {
		return "", err
	}
	if !m.HasSide(name) {
		return "", noSide(name)
	}
	return name, nil
}

// projectSides is the sides a local build command works on: the one named
// positionally, or every side the manifest declares.
func projectSides(p *project.Project, args []string) ([]string, error) {
	if len(args) == 0 {
		return p.Manifest.Sides(), nil
	}
	side, err := declaredSide(p.Manifest, args[0], "")
	if err != nil {
		return nil, err
	}
	return []string{side}, nil
}

// singleSide is the one side a command builds: the one --side names, or the
// only side the manifest declares.
func singleSide(p *project.Project, want string) (string, error) {
	if want != "" {
		return declaredSide(p.Manifest, want, "--side")
	}
	sides := p.Manifest.Sides()
	if len(sides) == 1 {
		return sides[0], nil
	}
	return "", ambiguousSide(sides, "--side")
}

// noClient is what a command that can take a remote source raises when the
// manifest declares no client; its user may not own the manifest, so the fix
// is the flag.
func noClient() error {
	e := out.Errorf("no-side", "%s declares no client", manifest.FileName)
	e.Rows = []out.Detail{{Label: "Fix", Text: "pass --assume-client to build one anyway"}}
	return e
}

// clientSide is the side a launcher-facing command builds: the declared
// client, or with --assume-client one built from what both sides share.
func (a *app) clientSide(m *manifest.Manifest, assume bool) (string, error) {
	if m.HasSide("client") {
		return "client", nil
	}
	if !assume {
		return "", noClient()
	}
	a.printer.Warn(assumeClientWarning)
	return "client", nil
}

// syncSide is the side sync builds: the client --assume-client stands in for,
// the one --side names, or the only declared side.
func (a *app) syncSide(p *project.Project, want string, assume bool) (string, error) {
	switch {
	case assume && want == "server":
		return "", out.Errorf("usage", "--assume-client builds the client; it doesn't go with --side server")
	case assume:
		return a.clientSide(p.Manifest, true)
	case want == "client" && !p.Manifest.HasSide("client"):
		return "", noClient()
	}
	return singleSide(p, want)
}

// exportSides is what export mrpack packs: every declared side, plus the
// client --assume-client stands in for, narrowed to the one --side names.
func (a *app) exportSides(m *manifest.Manifest, want string, assume bool) ([]string, error) {
	sides := m.Sides()
	if assume && !m.HasSide("client") {
		a.printer.Warn(assumeClientWarning)
		sides = append([]string{"client"}, sides...)
	}
	if want == "" {
		return sides, nil
	}
	if err := checkSide(want, "--side"); err != nil {
		return nil, err
	}
	if !slices.Contains(sides, want) {
		if want == "client" {
			return nil, noClient()
		}
		return nil, noSide(want)
	}
	return []string{want}, nil
}
