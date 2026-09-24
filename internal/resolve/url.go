package resolve

import (
	"context"
	"errors"
	"fmt"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// target is the project a URL names and the version it pins: a CurseForge file's project by its id,
// since the slug in the URL may be one CurseForge's search can't find.
func (r *Resolver) target(ctx context.Context, u provider.Ref) (project, version string, err error) {
	if u.Version == "" {
		return u.Project, "", nil
	}
	p, err := r.provider(u.Provider)
	if err != nil {
		return "", "", err
	}
	v, err := p.ProjectVersion(ctx, u.Project, u.Version)
	if errors.Is(err, provider.ErrNotFound) {
		return "", "", out.Errorf("version-not-found", "%s has no version %s for %s", u.Provider, u.Version, u.Project)
	}
	if err != nil {
		return "", "", err
	}
	return v.ProjectID, v.ID, nil
}

// FromURL is the slug and options that add what u names: its provider, and its version as the pin.
// Flags that name something else are refused.
func (r *Resolver) FromURL(ctx context.Context, u provider.Ref, opts AddOptions) (string, AddOptions, error) {
	if opts.Provider != "" && opts.Provider != u.Provider {
		return "", opts, out.Errorf("usage", "--provider %s disagrees with the %s URL", opts.Provider, u.Provider)
	}
	project, version, err := r.target(ctx, u)
	if err != nil {
		return "", opts, err
	}
	if version != "" && opts.Pin != "" && opts.Pin != version && opts.Pin != u.Version {
		return "", opts, out.Errorf("usage", "--pin %s disagrees with the URL's version %s", opts.Pin, u.Version)
	}
	opts.Provider, opts.IsFromURL = u.Provider, true
	if version != "" {
		opts.Pin = version
	}
	return project, opts, nil
}

// PinURL pins key to the version u names, which must be of the project key is locked from.
func (r *Resolver) PinURL(ctx context.Context, key string, u provider.Ref) (string, error) {
	if u.Version == "" {
		return "", out.Errorf("usage", "the URL names a project, not a version to pin %s to", key)
	}
	lockedProvider, lockedProject, ok := r.lockedSource(key)
	if !ok {
		if err := r.refuseLocalPin(key); err != nil {
			return "", err
		}
		if _, err := r.directTargets([]string{key}); err != nil {
			return "", err
		}
		return "", out.Errorf("usage", "%s is not locked from a provider", key)
	}
	if u.Provider != lockedProvider {
		return "", switchProject(out.Errorf("usage", "%s is locked from %s, not %s", key, lockedProvider, u.Provider), key)
	}
	project, version, err := r.target(ctx, u)
	if err != nil {
		return "", err
	}
	if project != lockedProject {
		return "", switchProject(out.Errorf("usage", "the URL's version belongs to project %s, not %s's project %s", project, key, lockedProject), key)
	}
	return r.Pin(ctx, key, version)
}

func switchProject(e *out.Error, key string) *out.Error {
	e.Help = fmt.Sprintf("to switch it, `shulker remove %s` and add the URL", key)
	return e
}

func (r *Resolver) lockedSource(key string) (providerName string, project string, ok bool) {
	if m, found := r.Lock.Mods[key]; found && m.Provider != "" {
		return m.Provider, m.Project, true
	}
	if m, found := r.Lock.Modpacks[key]; found && m.Provider != "" {
		return m.Provider, m.Project, true
	}
	return "", "", false
}
