package fetch

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
)

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
}

func New(version string) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 5 * time.Minute},
		UserAgent: fmt.Sprintf("shulker/%s (github.com/andrewmast/shulker)", version),
	}
}

func (c *Client) get(ctx context.Context, url string, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
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
