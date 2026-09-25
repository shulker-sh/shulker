package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

// The config.json keys whose values shulker checks beyond the schema.
const (
	AccountsStoresKey  = "accounts.stores"
	PlaySaveBackupsKey = "play.saveBackups"
	LogKeepDaysKey     = "log.keepDays"
)

// CheckValue rejects a value config.json can hold but shulker can't use, at the point it is typed
// rather than on the next run that reads it. stores is every account store shulker can read.
func CheckValue(key string, v any, stores []string) (any, error) {
	switch key {
	case AccountsStoresKey:
		names, err := storeList(v, stores)
		if err != nil {
			return nil, err
		}
		return names, nil
	case PlaySaveBackupsKey:
		return backupCount(key, v)
	case LogKeepDaysKey:
		return dayCount(key, v)
	}
	if name, ok := strings.CutPrefix(key, "play."); ok {
		if err := CheckPlaySetting(key, name, v); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// backupCount reads a count typed plainly or with --literal; 0 turns automatic backups off.
func backupCount(key string, v any) (any, error) {
	n, err := strconv.Atoi(fmt.Sprint(v))
	if err != nil || n < 0 {
		return nil, out.Errorf("usage", "%s takes a whole number of backups, 0 for none, not %s", key, valueText(v))
	}
	return n, nil
}

// dayCount reads a count of days typed plainly or with --literal. A log keeping no days would
// drop every earlier run on each write, so the least is 1.
func dayCount(key string, v any) (any, error) {
	n, err := strconv.Atoi(fmt.Sprint(v))
	if err != nil || n < 1 {
		return nil, out.Errorf("usage", "%s takes a whole number of days, at least 1, not %s", key, valueText(v))
	}
	return n, nil
}

// playHints say what each launch setting takes, for the error that refuses a value it can't.
var playHints = map[string]string{
	"memory":  `a heap size like "6G"`,
	"jvmArgs": `a list of JVM arguments; pass --literal '["-XX:+UseZGC"]'`,
	"java":    "an absolute path to a java binary or a Java home",
	"window":  `a window size like "1280x720"`,
	"wrapper": `a command as a list of words; pass --literal '["gamemoderun"]'`,
}

// CheckPlaySetting holds a launch setting to the rules of the instance key of the same name, which
// is the one the schema writes down, so a value an instance file would refuse can't be a default
// either. path is the key as it was typed, for the message.
func CheckPlaySetting(path, name string, v any) error {
	doc := map[string]any{"$schema": instance.SchemaURL, "settings": map[string]any{name: v}}
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	valid := schema.Validate(schema.Instance, data) == nil
	if java, ok := v.(string); ok && name == "java" && !filepath.IsAbs(java) {
		valid = false
	}
	if !valid {
		return out.Errorf("usage", "%s takes %s, not %s", path, playHints[name], valueText(v))
	}
	return nil
}

// storeList reads accounts.stores out of a JSON value.
func storeList(v any, stores []string) ([]string, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, out.Errorf("usage", "%s takes a JSON array of store names", AccountsStoresKey)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		name, ok := item.(string)
		if !ok {
			return nil, out.Errorf("usage", "%s takes store names, not %s", AccountsStoresKey, valueText(item))
		}
		names = append(names, name)
	}
	if err := CheckStores(names, stores); err != nil {
		return nil, err
	}
	return names, nil
}

// CheckStores refuses a store list shulker can't act on, wherever it was typed: an empty one
// leaves no account anywhere, a repeat says nothing the first mention didn't, and a name no
// launcher with a reader answers to is a typo rather than a launcher shulker hasn't reached yet.
func CheckStores(names, stores []string) error {
	if len(names) == 0 {
		e := out.Errorf("usage", "%s can't be empty", AccountsStoresKey)
		e.Help = "unset it to go back to the default"
		e.Candidates = stores
		return e
	}
	for i, name := range names {
		if slices.Contains(names[:i], name) {
			return out.Errorf("usage", "%s names %s twice", AccountsStoresKey, name)
		}
		if !slices.Contains(stores, name) {
			e := out.Errorf("usage", "shulker can't read accounts from %s", name)
			e.Candidates, e.Given = stores, name
			return e
		}
	}
	return nil
}

// valueText is a refused value as the message shows it: compact JSON with no HTML escaping.
func valueText(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// CheckDocument is whether doc, written as config.json, would read back; the error is the schema's.
func CheckDocument(doc map[string]any) error {
	written := maps.Clone(doc)
	written["$schema"] = schema.URL(schema.Config)
	data, err := json.Marshal(written)
	if err != nil {
		return err
	}
	return schema.Validate(schema.Config, data)
}
