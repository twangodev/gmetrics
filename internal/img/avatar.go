// Package img bundles small image-handling helpers that live outside any
// single plugin, most notably fetching a remote avatar and inlining it as a
// data: URL so the resulting SVG is fully self-contained.
package img

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
)

// FetchAvatar GETs url via hc and returns a "data:<content-type>;base64,..."
// URL suitable for embedding directly in an <image> element. The function
// returns an error if the request fails or the server replies with a
// non-200 status; callers that want to be tolerant of avatar fetch failures
// should treat the returned error as non-fatal.
func FetchAvatar(ctx context.Context, hc *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("avatar: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	mime := resp.Header.Get("Content-Type")
	if mime == "" {
		mime = "image/png"
	}
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(body)), nil
}
