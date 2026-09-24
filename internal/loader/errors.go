package loader

import (
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/out"
)

// fetchFailed is the meta-fetch error for a service that couldn't be reached or read, with the
// fetch error in a row labelled by the service. A network failure stays one for fetch.IsNetwork, so
// callers can fall back to what they already have.
func fetchFailed(err error, service, format string, args ...any) error {
	e := out.Errorf("meta-fetch", format, args...)
	e.WithCause(service, err)
	if fetch.IsNetwork(err) {
		return fetch.Unreachable(e)
	}
	return e
}

// invalid is the meta-invalid error for metadata that was read but lacks what shulker needs.
func invalid(format string, args ...any) *out.Error {
	return out.Errorf("meta-invalid", format, args...)
}

// unreadable is the meta-invalid error for metadata that won't parse, with the parser's error in a
// row labelled by the file it came from.
func unreadable(err error, file, format string, args ...any) *out.Error {
	return invalid(format, args...).WithCause(file, err)
}
