package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// remoteBlock is the least a RemoteFile asks for at once, so the many small reads a zip reader makes
// share a few requests. A read that carries on where the last block ended doubles the next block,
// up to remoteBlockMax, so a long zip directory also takes only a few.
const (
	remoteBlock    = 64 << 10
	remoteBlockMax = 4 << 20
)

// RemoteFile reads a file on a server by byte ranges, so a caller can pick one entry out of a
// large archive without downloading the rest. It keeps the last block it fetched.
type RemoteFile struct {
	c        *Client
	ctx      context.Context
	url      string
	size     int64
	block    []byte
	blockOff int64
	next     int64
}

// Remote opens url for ranged reads, asking the server for its size first.
func (c *Client) Remote(ctx context.Context, url string) (*RemoteFile, error) {
	resp, err := c.do(ctx, http.MethodHead, url, nil, nil)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.ContentLength < 0 {
		return nil, fmt.Errorf("%s: the server gives no size", url)
	}
	return &RemoteFile{c: c, ctx: ctx, url: url, size: resp.ContentLength, next: remoteBlock}, nil
}

// Size is the file's length in bytes, as the server gave it.
func (f *RemoteFile) Size() int64 { return f.size }

func (f *RemoteFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= f.size {
		return 0, io.EOF
	}
	want := min(int64(len(p)), f.size-off)
	if off < f.blockOff || off+want > f.blockOff+int64(len(f.block)) {
		if err := f.fetch(off, want); err != nil {
			return 0, err
		}
	}
	n := copy(p, f.block[off-f.blockOff:off-f.blockOff+want])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// fetch fills the block with at least a block's worth of bytes around [off, off+n), reaching back
// from the end of the file when the block would run past it.
func (f *RemoteFile) fetch(off, n int64) error {
	if f.block != nil && off >= f.blockOff && off <= f.blockOff+int64(len(f.block)) {
		f.next = min(2*f.next, remoteBlockMax)
	} else {
		f.next = remoteBlock
	}
	end := min(f.size, off+max(n, f.next))
	start := min(off, max(0, end-f.next))
	resp, err := f.c.do(f.ctx, http.MethodGet, f.url, http.Header{"Range": {fmt.Sprintf("bytes=%d-%d", start, end-1)}}, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("%s: the server ignored a range request (HTTP %d)", f.url, resp.StatusCode)
	}
	block := make([]byte, end-start)
	if _, err := io.ReadFull(resp.Body, block); err != nil {
		return fmt.Errorf("%s: %w", f.url, err)
	}
	f.block, f.blockOff = block, start
	return nil
}
