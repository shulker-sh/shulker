package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"shulker.sh/shulker/internal/out"
)

// readJSONObject reads a launcher file holding one JSON object. A missing file is an empty object
// that was not found, so a caller writing the file can start from it.
func readJSONObject(path string) (map[string]json.RawMessage, bool, error) {
	top := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return top, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, true, invalidFile(path, err)
	}
	return top, true, nil
}

func invalidFile(path string, err error) *out.Error {
	return out.Errorf("launcher-file-invalid", "shulker can't parse %s", path).WithCause("json", err)
}

// jsonObjectAt decodes the object top holds under key, empty when the key is absent. path is the
// file top came from, for the error.
func jsonObjectAt(path string, top map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	if raw, ok := top[key]; ok {
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, invalidFile(path, fmt.Errorf("%s: %w", key, err))
		}
	}
	return obj, nil
}

func jsonString(s string) json.RawMessage {
	data, _ := json.Marshal(s)
	return data
}
