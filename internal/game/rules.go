package game

import "regexp"

// Rule gates a library or an argument on the platform and on the features a launch turned on.
type Rule struct {
	Action   string          `json:"action"`
	OS       *OSRule         `json:"os,omitempty"`
	Features map[string]bool `json:"features,omitempty"`
}

type OSRule struct {
	Name    string `json:"name,omitempty"`
	Arch    string `json:"arch,omitempty"`
	Version string `json:"version,omitempty"`
}

// Allows applies a rule list the way the Mojang launcher does: with no rules everything is
// allowed, and otherwise every rule that matches sets the verdict, so the last match wins and
// anything the list never mentions is refused.
func Allows(rules []Rule, p Platform, features map[string]bool) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, r := range rules {
		if r.matches(p, features) {
			allowed = r.Action == "allow"
		}
	}
	return allowed
}

func (r Rule) matches(p Platform, features map[string]bool) bool {
	if r.OS != nil && !r.OS.matches(p) {
		return false
	}
	for name, want := range r.Features {
		if features[name] != want {
			return false
		}
	}
	return true
}

func (o OSRule) matches(p Platform) bool {
	if o.Name != "" && o.Name != p.OS {
		return false
	}
	if o.Arch != "" && o.Arch != p.Arch {
		return false
	}
	if o.Version == "" {
		return true
	}
	if p.Version == "" {
		return false
	}
	re, err := regexp.Compile(o.Version)
	return err == nil && re.MatchString(p.Version)
}
