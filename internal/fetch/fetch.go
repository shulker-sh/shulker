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
	"time"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
	ErrOffline   = errors.New("not using the network (--offline)")
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

type StatusError struct {
	URL    string
	Status int
}

func (e *StatusError) Error() string { return fmt.Sprintf("%s: HTTP %d", e.URL, e.Status) }

func (e *StatusError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrForbidden:
		return e.Status == http.StatusForbidden
	}
	return false
}

type Client struct {
	HTTP      *http.Client
	UserAgent string
	Header    http.Header
	Offline   bool
}

func New(version string) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 5 * time.Minute},
		UserAgent: fmt.Sprintf("shulker/%s (https://shulker.sh)", version),
	}
}

func (c *Client) get(ctx context.Context, url string, accept string) (*http.Response, error) {
	return c.do(ctx, http.MethodGet, url, accept, nil)
}

func (c *Client) do(ctx context.Context, method, url, accept string, body io.Reader) (*http.Response, error) {
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
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		resp.Body.Close()
		return nil, &StatusError{URL: url, Status: resp.StatusCode}
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
	resp, err := c.do(ctx, http.MethodPost, url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("%s: decode: %w", url, err)
	}
	return nil
}

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

func (c *Client) Download(ctx context.Context, url string, dst io.Writer) (string, error) {
	resp, err := c.get(ctx, url, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	h := sha512.New()
	if _, err := io.Copy(io.MultiWriter(dst, h), resp.Body); err != nil {
		return "", fmt.Errorf("%s: %w", url, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
