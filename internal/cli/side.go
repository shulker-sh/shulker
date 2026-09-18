package cli

import (
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

var sideNames = []string{"client", "server"}

func isSide(name string) bool { return name == "client" || name == "server" }

// checkSide reads a side out of a positional argument or the --target shim.
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

// singleSide is the one side a command builds: the one --target names, or the
// only side the manifest declares.
func singleSide(p *project.Project, want string) (string, error) {
	if want != "" {
		return declaredSide(p.Manifest, want, "--target")
	}
	sides := p.Manifest.Sides()
	if len(sides) == 1 {
		return sides[0], nil
	}
	e := out.Errorf("ambiguous-target", "%s declares both sides; pass --target", manifest.FileName)
	e.Candidates, e.Flag = sides, "--target"
	return "", e
}

// sideOf is the side a launcher-facing command needs, which --target may name
// as long as it names that one.
func sideOf(m *manifest.Manifest, want, side, verb string) (string, error) {
	if want != "" {
		if err := checkSide(want, "--target"); err != nil {
			return "", err
		}
		if want != side {
			e := out.Errorf("wrong-side-target", "--target names the %s side; %s needs the %s side", want, verb, side)
			e.Candidates, e.Given, e.Flag = []string{side}, want, "--target"
			return "", e
		}
	}
	if !m.HasSide(side) {
		return "", noSide(side)
	}
	return side, nil
}
