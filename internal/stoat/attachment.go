package stoat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
)

// maxDownloadBytes caps how much of a remote CDN response is read before
// upload, mirroring Autumn's own attachment size cap (20MB, confirmed live
// against the test instance, Phase 0) -- a larger file would fail the
// Autumn upload anyway, so this bounds memory use instead of buffering an
// oversized body first.
const maxDownloadBytes = 20 * 1024 * 1024

// UploadFromURL downloads the file at rawURL (a Discord CDN link -- these
// are time-limited and cannot be linked to directly, spec.md attachments)
// and re-uploads it to Stoat's Autumn file service under tag, returning the
// Autumn-assigned attachment id. Shared machinery for attachment re-upload
// (5.3, tag "attachments") and custom emoji creation (5.4, tag "emojis").
func (c *Client) UploadFromURL(ctx context.Context, tag, rawURL string) (string, error) {
	data, err := downloadLimited(ctx, c.inner.HTTP, rawURL, maxDownloadBytes)
	if err != nil {
		return "", err
	}

	id, err := c.inner.Upload(ctx, tag, filenameFromURL(rawURL), data)
	if err != nil {
		return "", fmt.Errorf("stoat: upload %s to Autumn tag %s: %w", rawURL, tag, err)
	}
	return id, nil
}

// downloadLimited fetches rawURL and reads its body up to limit bytes,
// erroring rather than silently truncating if the body is larger.
func downloadLimited(ctx context.Context, httpClient *http.Client, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("stoat: build download request for %s: %w", rawURL, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stoat: download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stoat: download %s: unexpected status %s", rawURL, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("stoat: read download body for %s: %w", rawURL, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("stoat: download %s exceeds %d byte limit", rawURL, limit)
	}
	return data, nil
}

// filenameFromURL extracts the final path segment (query string stripped)
// to use as the multipart form filename Autumn stores.
func filenameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return path.Base(rawURL)
	}
	return path.Base(u.Path)
}
