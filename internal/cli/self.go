package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/selfupdate"
)

type selfUpdateResult struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Available  bool   `json:"available"`
	Updated    bool   `json:"updated"`
	Path       string `json:"path,omitempty"`
	Provenance string `json:"provenance,omitempty"`
}

func (a *app) selfCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "self",
		Short: "Manage the shulker binary itself",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(a.selfUpdateCmd())
	return cmd
}

func (a *app) selfUpdateCmd() *cobra.Command {
	var check, without, require bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update shulker to the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.selfUpdate(cmd.Context(), check, without, require)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only report whether a newer release is available")
	cmd.Flags().BoolVar(&without, "without-attestation", false, "skip the build provenance check")
	cmd.Flags().BoolVar(&require, "require-attestation", false, "fail unless gh verifies the build provenance")
	cmd.MarkFlagsMutuallyExclusive("without-attestation", "require-attestation")
	return cmd
}

func (a *app) selfUpdate(ctx context.Context, check, without, require bool) error {
	r := a.releases
	if r == nil {
		r = selfupdate.New(fetch.New(version))
	}
	tag, err := r.Latest(ctx)
	switch {
	case errors.Is(err, fetch.ErrNotFound):
		return out.Errorf("update-check", "no shulker release has been published yet")
	case fetch.IsNetwork(err):
		return out.Errorf("update-check", "can't reach GitHub to check for updates: %v", err)
	case err != nil:
		return out.Errorf("update-check", "check for updates: %v", err)
	}
	res := selfUpdateResult{Current: version, Latest: strings.TrimPrefix(tag, "v"), Available: selfupdate.NeedsUpdate(version, tag)}
	if !res.Available {
		return a.printer.Emit(res, func(w io.Writer) { fmt.Fprintf(w, "shulker is up to date (%s)\n", version) })
	}
	if check {
		return a.printer.Emit(res, func(w io.Writer) {
			fmt.Fprintf(w, "shulker %s is available (you have %s); run shulker self update to install it\n", res.Latest, version)
		})
	}

	exe, err := a.exe()
	if err != nil {
		return out.Errorf("update-install", "find the running shulker binary: %v", err)
	}
	tmp, err := os.MkdirTemp("", "shulker-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	a.progress("downloading shulker %s", res.Latest)
	archive, err := r.Download(ctx, tag, tmp)
	if err != nil {
		var sum *selfupdate.ChecksumError
		if errors.As(err, &sum) {
			return out.Errorf("update-checksum", "%v", err)
		}
		return out.Errorf("update-download", "download shulker %s: %v", res.Latest, err)
	}
	a.progress("checksum verified")
	if res.Provenance, err = a.checkProvenance(ctx, r, tag, archive, without, require); err != nil {
		return err
	}
	if err := selfupdate.Install(archive, exe); err != nil {
		return out.Errorf("update-install", "replace %s: %v", exe, err)
	}
	res.Updated, res.Path = true, exe
	return a.printer.Emit(res, func(w io.Writer) {
		fmt.Fprintf(w, "updated shulker %s -> %s at %s\n", version, res.Latest, exe)
	})
}

func (a *app) checkProvenance(ctx context.Context, r *selfupdate.Releases, tag, archive string, without, require bool) (string, error) {
	if without {
		a.progress("skipping build provenance check (--without-attestation)")
		return "skipped", nil
	}
	if _, err := exec.LookPath("gh"); err != nil {
		if require {
			return "", out.Errorf("update-provenance", "--require-attestation is set but gh is not installed")
		}
		a.progress("gh not found, skipping build provenance check")
		return "skipped", nil
	}
	a.progress("verifying build provenance with gh")
	if err := r.VerifyProvenance(ctx, tag, archive); err != nil {
		if require {
			return "", out.Errorf("update-provenance", "build provenance could not be verified: %v", err)
		}
		a.progress("build provenance could not be verified, continuing on the checksum")
		return "unverified", nil
	}
	a.progress("build provenance verified")
	return "verified", nil
}
