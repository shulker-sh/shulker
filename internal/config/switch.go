package config

import (
	"fmt"
	"path/filepath"

	"shulker.sh/shulker/internal/out"
)

// SwitchRegistry checks the registry next resolves to, creating it when missing, and returns its
// path when it did. Without force it refuses when the current registry holds entries the new one
// lacks, because shulker would stop syncing them.
func SwitchRegistry(configPath string, current, next Config, force bool) (string, error) {
	from := RegistryPath(configPath, current)
	to := RegistryPath(configPath, next)
	if filepath.Clean(from) == filepath.Clean(to) {
		return "", nil
	}
	dest, err := LoadInstances(to)
	if err != nil {
		return "", err
	}
	if !force {
		instances, err := LoadInstances(from)
		if err != nil {
			return "", err
		}
		var left []string
		for _, in := range instances {
			if _, ok := FindInstance(dest, in.Dir); !ok {
				left = append(left, in.Dir)
			}
		}
		if len(left) > 0 {
			entries := fmt.Sprintf("%d instances", len(left))
			if len(left) == 1 {
				entries = "1 instance"
			}
			e := out.Errorf("registry-has-instances", "changing the registry leaves %s behind in %s", entries, from)
			e.Help = "run again with --force to change it anyway"
			e.Items = left
			return "", e
		}
	}
	created, err := CreateRegistry(to)
	if err != nil || !created {
		return "", err
	}
	return to, nil
}
