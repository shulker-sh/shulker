package cli

import (
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

// noSide is what a command raises when the manifest declares no block for the
// side it needs.
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

// clientSide is the side a launcher-facing command builds, which the manifest
// must declare.
func clientSide(m *manifest.Manifest) (string, error) {
	if !m.HasSide("client") {
		return "", noSide("client")
	}
	return "client", nil
}
