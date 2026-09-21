package launcher

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

func readINILines(path string) ([]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, strings.TrimRight(sc.Text(), "\r"))
	}
	return lines, sc.Err()
}

func readINI(path string, unescape func(string) string) (map[string]string, error) {
	lines, err := readINILines(path)
	if err != nil {
		return nil, err
	}
	if lines == nil {
		return nil, os.ErrNotExist
	}
	values := map[string]string{}
	for _, line := range lines {
		if key, value, ok := splitINILine(line); ok {
			values[key] = unescape(value)
		}
	}
	return values, nil
}

func splitINILine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	key, value, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), true
}
