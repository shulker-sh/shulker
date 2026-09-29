package resolve

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"shulker.sh/shulker/internal/env"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/security"
)

// Age is how old a version is against security.minReleaseAge: when it was published, how many
// whole days ago, and when it becomes old enough to be chosen.
type Age struct {
	Published time.Time `json:"published"`
	AgeDays   int       `json:"ageDays"`
	Qualifies time.Time `json:"qualifies"`
}

func ageOf(published, now time.Time, minAge time.Duration) Age {
	return Age{Published: published, AgeDays: max(security.Days(now.Sub(published)), 0), Qualifies: published.Add(minAge)}
}

// Text is how old the version is and the day it qualifies.
func (a Age) Text() string { return a.old() + ", qualifies " + dateText(a.Qualifies) }

func (a Age) old() string {
	if a.AgeDays == 0 {
		return "published today"
	}
	return out.Count(a.AgeDays, "day", "days") + " old"
}

// Held is a version security.minReleaseAge held back: the newest one an entry could take, skipped
// for an older one because it was published too recently. Its Age is the skipped version's.
type Held struct {
	Key  string `json:"key"`
	Took string `json:"took"`
	// Skipped is the held-back version's number, and SkippedID the id `shulker pin` takes.
	Skipped   string `json:"skipped"`
	SkippedID string `json:"skippedId"`
	Age
	provider string
	project  string
}

// Young is a version taken although it is younger than security.minReleaseAge: a pinned one, or
// one a source's lock holds.
type Young struct {
	Key     string `json:"key"`
	Version string `json:"version"`
	Age
}

// ReleaseAge is the release-age facts of a warning under --json.
type ReleaseAge struct {
	MinReleaseAge int     `json:"minReleaseAge"`
	Held          []Held  `json:"held,omitempty"`
	Young         []Young `json:"young,omitempty"`
}

func (r *Resolver) now() time.Time { return env.Clock(r.Now) }

// cutoff is the latest a version may be published and still be chosen.
func (r *Resolver) cutoff() time.Time { return r.now().Add(-r.MinReleaseAge) }

// choose is the newest of versions on channel published by the cutoff, never older than the
// version the lock held for the project before this run. A newer version it skips is recorded
// as held back under key, or under the key the project is locked by when key is empty. ok is
// false when versions hold nothing on channel at all; when they do but all are too young, it
// fails release-too-new.
func (r *Resolver) choose(p provider.Provider, proj *provider.Project, key string, versions []provider.Version, q versionQuery, channel string) (provider.Version, bool, error) {
	took, skipped, ok := provider.NewestBy(versions, channel, q.loader, r.cutoff())
	if floor, found := r.floor(p.Name(), proj.ID, versions, channel); found && (!ok || floor.Published.After(took.Published)) {
		took, ok = floor, true
	}
	if skipped == nil {
		return took, ok, nil
	}
	if !ok {
		return provider.Version{}, false, r.tooNew(proj.Slug, *skipped)
	}
	if skipped.ID != took.ID {
		r.held = append(r.held, r.heldOf(p.Name(), proj.ID, key, took, *skipped))
	}
	return took, true, nil
}

// floor is the version the lock held for providerName's project before this run, among versions
// and allowed on channel.
func (r *Resolver) floor(providerName, projectID string, versions []provider.Version, channel string) (provider.Version, bool) {
	locked, ok := r.floors[providerName+"/"+projectID]
	if !ok {
		return provider.Version{}, false
	}
	i := slices.IndexFunc(versions, func(v provider.Version) bool { return v.ID == locked })
	if i < 0 || !provider.ChannelAllows(channel, versions[i].Channel) {
		return provider.Version{}, false
	}
	return versions[i], true
}

// holdFloors records every provider version l locks, which a choice made after never moves back
// from.
func (r *Resolver) holdFloors(l *lock.Lock) {
	r.floors = map[string]string{}
	for _, e := range l.ProviderEntries() {
		r.floors[e.Provider+"/"+e.Project] = e.Version
	}
}

// PinNudge is the command that takes the held-back version now.
func (h Held) PinNudge() out.Nudge {
	return out.Nudge{Lead: "Take " + h.Key + " " + h.Skipped + " now", Command: "shulker pin " + h.Key + " " + h.SkippedID}
}

func (r *Resolver) heldOf(providerName, projectID, key string, took, skipped provider.Version) Held {
	return Held{
		Key:       key,
		Took:      took.Number,
		Skipped:   skipped.Number,
		SkippedID: skipped.ID,
		Age:       ageOf(skipped.Published, r.now(), r.MinReleaseAge),
		provider:  providerName,
		project:   projectID,
	}
}

// tooNew is the refusal for a project whose versions on the channel are all too young.
func (r *Resolver) tooNew(slug string, newest provider.Version) *out.Error {
	age := ageOf(newest.Published, r.now(), r.MinReleaseAge)
	e := out.Errorf("release-too-new", "%s has no version older than %s; its newest, %s, is %s", slug, dayText(r.MinReleaseAge), newest.Number, age.old())
	e.Help = fmt.Sprintf("it qualifies on %s; to take it now, pin it with `shulker add %s --pin %s`", dateText(age.Qualifies), slug, newest.ID)
	return security.Refusal(security.ReleaseAge, e)
}

// pinnedYoung notes a pinned version younger than the release age, which is taken all the same,
// under key or, before the entry has one, proj's slug.
func (r *Resolver) pinnedYoung(key string, proj *provider.Project, v *provider.Version) {
	if !v.Published.After(r.cutoff()) {
		return
	}
	if key == "" {
		key = proj.Slug
	}
	r.young = append(r.young, Young{Key: key, Version: v.Number, Age: ageOf(v.Published, r.now(), r.MinReleaseAge)})
}

// Held is every version this run held back, each under the key the lock holds it by; one whose
// project ended up out of the lock is left out.
func (r *Resolver) Held() []Held {
	var held []Held
	for _, h := range r.held {
		if h.Key == "" {
			key, ok := r.lockedProject(h.provider, h.project)
			if !ok {
				continue
			}
			h.Key = key
		}
		if i := slices.IndexFunc(held, func(o Held) bool { return o.Key == h.Key }); i >= 0 {
			held[i] = h
			continue
		}
		held = append(held, h)
	}
	return held
}

// AgeWarnings are the warnings this run's choices raise under security.minReleaseAge: the versions
// it held back, and each pinned version taken although it is too young.
func (r *Resolver) AgeWarnings() []out.SecurityWarning {
	var warnings []out.SecurityWarning
	if held := r.Held(); len(held) > 0 {
		warnings = append(warnings, HeldWarning(held, r.MinReleaseAge))
	}
	for _, y := range r.young {
		msg := fmt.Sprintf("%s %s is pinned, so it is taken although it is %s, younger than the %s security.minReleaseAge asks.", y.Key, y.Version, y.old(), dayText(r.MinReleaseAge))
		warnings = append(warnings, security.Warn(security.ReleaseAge, msg, ReleaseAge{MinReleaseAge: security.Days(r.MinReleaseAge), Young: []Young{y}}))
	}
	return warnings
}

// HeldWarning says which versions were held back, how old each is and when it qualifies, and how
// to take one now.
func HeldWarning(held []Held, minAge time.Duration) out.SecurityWarning {
	lines := []string{fmt.Sprintf("Held back %s younger than %s.", out.Count(len(held), "version", "versions"), dayText(minAge))}
	for _, h := range held {
		aside := h.Text()
		if len(held) > 1 {
			aside = "id " + h.SkippedID + ", " + aside
		}
		lines = append(lines, fmt.Sprintf("%s: took %s over %s (%s)", h.Key, h.Took, h.Skipped, aside))
	}
	take := out.Nudge{Lead: "Take one now", Command: "shulker pin <mod> <id>"}
	if len(held) == 1 {
		take = held[0].PinNudge()
	}
	return security.Warn(security.ReleaseAge, strings.Join(lines, "\n"), ReleaseAge{MinReleaseAge: security.Days(minAge), Held: held}, take)
}

// YoungEntries are the provider entries l locks that were published less than minAge before now.
func YoungEntries(l *lock.Lock, minAge time.Duration, now time.Time) []Young {
	if minAge <= 0 {
		return nil
	}
	var young []Young
	for _, e := range l.ProviderEntries() {
		if !e.Published.IsZero() && e.Published.After(now.Add(-minAge)) {
			young = append(young, Young{Key: e.Key, Version: e.VersionNumber, Age: ageOf(e.Published, now, minAge)})
		}
	}
	return young
}

// YoungWarning says a source's lock holds versions younger than minAge, which are installed as
// its author locked them.
func YoungWarning(young []Young, minAge time.Duration) out.SecurityWarning {
	lines := []string{fmt.Sprintf("The source locks %s younger than %s, installed as its author chose them.", out.Count(len(young), "version", "versions"), dayText(minAge))}
	for _, y := range young {
		lines = append(lines, fmt.Sprintf("%s %s (%s)", y.Key, y.Version, y.Text()))
	}
	return security.Warn(security.ReleaseAge, strings.Join(lines, "\n"), ReleaseAge{MinReleaseAge: security.Days(minAge), Young: young})
}

func dayText(d time.Duration) string { return out.Count(security.Days(d), "day", "days") }

func dateText(t time.Time) string { return t.Local().Format("2006-01-02") }
