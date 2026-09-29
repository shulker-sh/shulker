package cli

import (
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/security"
)

type securityInfo struct {
	Stance      string                `json:"stance"`
	Protections []security.Protection `json:"protections"`
}

func (a *app) securityCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "security",
		Annotations: reads(),
		Short:       "Explain how shulker keeps bad files off your machine",
		Long:        "Explain how shulker keeps bad files off your machine: what it checks on every run, what each check stops, and the settings that change them. Every security warning and error points here.",
		Args:        noArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			info := securityInfo{Stance: security.Stance, Protections: security.Protections()}
			return a.printer.Emit(info, func(l *out.Lines) { printSecurity(l, info) })
		},
	}
}

func printSecurity(l *out.Lines, info securityInfo) {
	l.Paragraph(info.Stance)
	l.Blank()
	l.Paragraph("Shulker does all of this on every run, and nothing in a project or source can turn it off:")
	var settings [][]string
	for _, p := range info.Protections {
		if p.Setting != "" {
			settings = append(settings, []string{p.Setting, p.Value, p.Changes})
			continue
		}
		l.Bullet(p.Summary)
	}
	if len(settings) == 0 {
		return
	}
	l.Blank()
	l.Heading("Settings")
	l.Table([]string{"Setting", "Value", "What it changes"}, settings, out.Columns(l.T.StyleCommand(), l.T.Style()))
}
