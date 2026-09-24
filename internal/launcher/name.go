package launcher

import (
	"encoding/json"
	"os"
)

// InstanceName is the name the launcher shows for an instance, read back from the file shulker wrote
// it into, or empty when there is none to read. An unreadable file reads as empty rather than
// failing, since whoever asks is about to name the instance around it.
func (e *Entry) InstanceName(launcherDir, gameDir string) string {
	if e == nil || e.name == nil {
		return ""
	}
	return e.name(e, launcherDir, gameDir)
}

func readJSON(path string, v any) {
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, v)
	}
}
