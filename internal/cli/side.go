package cli

import (
	"slices"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

func checkSide(name, flag string) error {
	if manifest.IsSide(name) {
		return nil
	}
	e := manifest.NotASide(name)
	e.Flag = flag
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
	e := out.Errorf("ambiguous-side", "%s declares both sides", manifest.FileName)
	e.Help = "choose one"
	e.Candidates, e.Flag = sides, flag
	return e
}

func declaredSide(m *manifest.Manifest, name, flag string) (string, error) {
	if err := checkSide(name, flag); err != nil {
		return "", err
	}
	if !m.HasSide(name) {
		return "", noSide(name)
	}
	return name, nil
}

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

func (a *app) syncSide(p *project.Project, want string, assume bool) (string, error) {
	if p.Manifest.HasSide("client") {
		assume = false
	}
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
