package auditlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"time"

	"shulker.sh/shulker/internal/fsutil"
)

// MaxSize is the most the log holds, whatever the age of its entries. On Windows the log sits in
// %AppData%, which roams, so this is also what a roaming profile carries between machines.
const MaxSize = 4 << 20

// Trim drops the entries older than keepDays days before now and, past MaxSize, the oldest of the
// rest. A log with nothing to drop is read no further than its first line.
func Trim(path string, keepDays int, now time.Time) error {
	return trim(path, keepDays, now, MaxSize)
}

func trim(path string, keepDays int, now time.Time, maxSize int64) error {
	if info, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) || err == nil && info.Size() == 0 {
		return nil
	}
	unlock, err := fsutil.Lock(lockPath(path))
	if err != nil {
		return err
	}
	defer unlock()
	cutoff := now.AddDate(0, 0, -keepDays)
	data, err := readStale(path, cutoff, maxSize)
	if err != nil || data == nil {
		return err
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	start := len(lines)
	for i, line := range lines {
		if within(line, cutoff) {
			start = i
			break
		}
	}
	kept, size := lines[start:], int64(len(data))
	for _, line := range lines[:start] {
		size -= int64(len(line))
	}
	// Trimming to under the cap rather than to it leaves room for the next runs, so a log at the
	// cap isn't rewritten by every one of them.
	for size > maxSize*3/4 && len(kept) > 0 {
		size -= int64(len(kept[0]))
		kept = kept[1:]
	}
	if len(kept) == len(lines) {
		return nil
	}
	return fsutil.Write(path, bytes.Join(kept, nil))
}

// readStale is the whole log when something in it is due to go, and nil when its first entry is
// inside the window and it is under maxSize, since entries are appended oldest first.
func readStale(path string, cutoff time.Time, maxSize int64) ([]byte, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(f)
	first, err := r.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	if info.Size() <= maxSize && within(first, cutoff) {
		return nil, nil
	}
	rest, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return append(first, rest...), nil
}

// within is whether line is an entry written at or after cutoff. A line with no time can't be
// placed, so it goes with the old ones around it.
func within(line []byte, cutoff time.Time) bool {
	var e struct {
		At string `json:"at"`
	}
	if json.Unmarshal(line, &e) != nil {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, e.At)
	return err == nil && !at.Before(cutoff)
}

// lockPath is the file every writer of the log at path holds while it writes, so a trim never
// replaces the log under an append.
func lockPath(path string) string {
	return path + ".lock"
}
