// Package fetch is shulker's HTTP client: every request it makes goes through one Client, which
// honours --offline and tells network failures apart from a server's answer.
package fetch

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrForbidden   = errors.New("forbidden")
	ErrRateLimited = errors.New("rate limited")
	ErrOffline     = errors.New("not using the network (--offline)")
)

type unreachableError struct{ err error }

func (e *unreachableError) Error() string { return e.err.Error() }
func (e *unreachableError) Unwrap() error { return e.err }

// Unreachable marks err as a network failure for IsNetwork, for callers that learn it from
// something other than a Go network error, such as git's stderr.
func Unreachable(err error) error { return &unreachableError{err} }

// IsNetwork reports whether err means the network couldn't be reached (DNS, connect, TLS,
// timeout, or --offline), as opposed to a server that answered with an error.
func IsNetwork(err error) bool {
	var ue *unreachableError
	if errors.Is(err, ErrOffline) || errors.As(err, &ue) {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var urlErr *url.Error
	var netErr net.Error
	return errors.As(err, &urlErr) || errors.As(err, &netErr)
}

// StatusError is a server answering with a status other than 2xx. errors.Is matches a 404 against
// ErrNotFound, a 403 against ErrForbidden and a 429 against ErrRateLimited.
type StatusError struct {
	URL    string
	Status int
	// RetryAfter is how long a 429 said to wait, from Retry-After or Modrinth's X-Ratelimit-Reset;
	// zero when it didn't say.
	RetryAfter time.Duration
	// Body is what the server answered with, up to errorBody bytes. The sign-in chain reads it:
	// its endpoints put the reason a request failed in a JSON body, not in the status.
	Body []byte
}

// errorBody is how much of a failed request's answer is kept for the caller to read.
const errorBody = 64 << 10

func (e *StatusError) Error() string { return fmt.Sprintf("%s: HTTP %d", e.URL, e.Status) }

func (e *StatusError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrForbidden:
		return e.Status == http.StatusForbidden
	case ErrRateLimited:
		return e.Status == http.StatusTooManyRequests
	}
	return false
}

type Client struct {
	HTTP      *http.Client
	UserAgent string
	Header    http.Header
	Offline   bool
	// Progress, when set, receives every chunk of download body bytes.
	Progress func(n int64)
	// Waiting, when set, is told the host of each request as it starts; the func it returns is
	// called once the response body is closed, or at once when the request fails.
	Waiting func(host string) (done func())
}

type waitingBody struct {
	io.ReadCloser
	done func()
}

func (b waitingBody) Close() error {
	b.done()
	return b.ReadCloser.Close()
}

type countingWriter struct {
	w io.Writer
	f func(n int64)
}

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 && c.f != nil {
		c.f(int64(n))
	}
	return n, err
}

func New(version string) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 5 * time.Minute},
		UserAgent: fmt.Sprintf("shulker/%s (https://shulker.sh)", version),
	}
}

func (c *Client) get(ctx context.Context, url string, accept string) (*http.Response, error) {
	return c.do(ctx, http.MethodGet, url, accepting(accept), nil)
}

func accepting(accept string) http.Header {
	if accept == "" {
		return nil
	}
	return http.Header{"Accept": {accept}}
}

func (c *Client) do(ctx context.Context, method, url string, header http.Header, body io.Reader) (*http.Response, error) {
	if c.Offline {
		return nil, fmt.Errorf("%s: %w", url, ErrOffline)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	for k, vs := range c.Header {
		req.Header[k] = vs
	}
	for k, vs := range header {
		req.Header[k] = vs
	}
	done := func() {}
	if c.Waiting != nil {
		done = c.Waiting(req.URL.Hostname())
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		done()
		return nil, err
	}
	resp.Body = waitingBody{resp.Body, done}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, errorBody))
		resp.Body.Close()
		return nil, &StatusError{URL: url, Status: resp.StatusCode, Body: answer, RetryAfter: retryAfter(resp)}
	}
	return resp, nil
}

func (c *Client) GetJSON(ctx context.Context, url string, v any) error {
	resp, err := c.get(ctx, url, "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("%s: decode: %w", url, err)
	}
	return nil
}

func (c *Client) GetXML(ctx context.Context, url string, v any) error {
	resp, err := c.get(ctx, url, "application/xml")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := xml.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("%s: decode: %w", url, err)
	}
	return nil
}

func (c *Client) PostJSON(ctx context.Context, url string, body any, v any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPost, url, http.Header{"Accept": {"application/json"}, "Content-Type": {"application/json"}}, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("%s: decode: %w", url, err)
	}
	return nil
}

// PostForm posts a form-encoded body and decodes the JSON answer, which is what the Microsoft
// token endpoints take and give.
func (c *Client) PostForm(ctx context.Context, url string, form url.Values, v any) error {
	resp, err := c.do(ctx, http.MethodPost, url, http.Header{"Accept": {"application/json"}, "Content-Type": {"application/x-www-form-urlencoded"}}, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("%s: decode: %w", url, err)
	}
	return nil
}

// GetJSONIfFound is GetJSON where a 404 or a 204 is an answer, not an error: it reports whether v
// was filled.
func (c *Client) GetJSONIfFound(ctx context.Context, url string, v any) (bool, error) {
	resp, err := c.get(ctx, url, "application/json")
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return false, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return false, fmt.Errorf("%s: decode: %w", url, err)
	}
	return true, nil
}

// Download copies url's body into dst and returns its sha512 in hex.
func (c *Client) Download(ctx context.Context, url string, dst io.Writer) (string, error) {
	resp, err := c.get(ctx, url, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	h := sha512.New()
	var w io.Writer = io.MultiWriter(dst, h)
	if c.Progress != nil {
		w = countingWriter{w, c.Progress}
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return "", fmt.Errorf("%s: %w", url, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func retryAfter(resp *http.Response) time.Duration {
	if resp.StatusCode != http.StatusTooManyRequests {
		return 0
	}
	for _, header := range []string{"Retry-After", "X-Ratelimit-Reset"} {
		if seconds, err := strconv.Atoi(resp.Header.Get(header)); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return 0
}
