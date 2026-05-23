package music

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/twangodev/gmetrics/internal/img"
	"github.com/twangodev/gmetrics/internal/plugin"
)

// lastfmResponse mirrors the shape of the Last.fm
// user.getrecenttracks/format=json payload. Field names with leading "@"
// and "#" characters are awkward in Go but legal as JSON tags; we model
// them with explicit `json:"@attr"` / `json:"#text"` tags so the decoder
// finds them.
type lastfmResponse struct {
	RecentTracks struct {
		Track []lastfmTrack `json:"track"`
	} `json:"recenttracks"`
}

// lastfmTrack is one entry inside recenttracks.track. The Last.fm API
// returns artist as an object with a "#text" field (the artist name) and
// date as an object whose "uts" is the play timestamp as a Unix-seconds
// string. The optional "@attr" object only appears when the track is
// currently playing.
type lastfmTrack struct {
	Name   string `json:"name"`
	Artist struct {
		Text string `json:"#text"`
	} `json:"artist"`
	Image []lastfmImage `json:"image"`
	Date  struct {
		UTS string `json:"uts"`
	} `json:"date"`
	Attr struct {
		NowPlaying string `json:"nowplaying"`
	} `json:"@attr"`
}

// lastfmImage is one entry in track.image. Size is the canonical Last.fm
// label ("small", "medium", "large", "extralarge", "mega"); the URL is
// nested under the "#text" key.
type lastfmImage struct {
	Text string `json:"#text"`
	Size string `json:"size"`
}

// fetchLastfm issues a single GET against the user.getrecenttracks Last.fm
// endpoint, decodes the response, and assembles a Data value. Avatars
// (artwork) are fetched only when env.HTTP is non-nil and the largest
// image URL is non-empty; any artwork fetch failure is logged and the
// track is kept with an empty ArtworkB64 so the rest of the card still
// renders. Tests pass env.HTTP == nil to skip the artwork step.
func fetchLastfm(ctx context.Context, env *plugin.Env, cfg Config) (Data, error) {
	base := cfg.URL
	if base == "" {
		base = defaultLastfmURL
	}
	endpoint, err := buildLastfmURL(base, cfg)
	if err != nil {
		return Data{}, fmt.Errorf("music: build url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Data{}, fmt.Errorf("music: new request: %w", err)
	}
	// Match upstream's UA so Last.fm doesn't shadowban a misconfigured
	// client. The Accept header is explicit so the server doesn't fall
	// back to XML in some edge cases.
	req.Header.Set("User-Agent", "gmetrics")
	req.Header.Set("Accept", "application/json")

	hc := env.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Data{}, fmt.Errorf("music: lastfm get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Data{}, fmt.Errorf("music: lastfm api returned %s: %s", resp.Status, string(body))
	}

	var parsed lastfmResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return Data{}, fmt.Errorf("music: decode lastfm response: %w", err)
	}

	data := Data{Mode: cfg.Mode, Provider: cfg.Provider}
	for _, raw := range parsed.RecentTracks.Track {
		t := Track{
			Name:     raw.Name,
			Artist:   raw.Artist.Text,
			PlayedAt: formatPlayedAt(raw),
		}
		artworkURL := largestImageURL(raw.Image)
		if env.HTTP != nil && artworkURL != "" {
			b64, err := img.FetchAvatar(ctx, env.HTTP, artworkURL)
			if err != nil {
				if env.Log != nil {
					env.Log.Warn("music: artwork fetch failed",
						"track", t.Name, "url", artworkURL, "err", err)
				}
			} else {
				t.ArtworkB64 = b64
			}
		}
		data.Tracks = append(data.Tracks, t)
	}
	return data, nil
}

// buildLastfmURL combines the (possibly overridden) base URL with the
// fixed user.getrecenttracks query parameters. We use net/url so user
// names and tokens with reserved characters are percent-encoded correctly.
func buildLastfmURL(base string, cfg Config) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("method", "user.getrecenttracks")
	q.Set("user", cfg.User)
	q.Set("api_key", cfg.Token)
	q.Set("format", "json")
	if cfg.Limit > 0 {
		q.Set("limit", strconv.Itoa(cfg.Limit))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// formatPlayedAt converts the per-track timestamp data into the
// pre-formatted display string Render expects. The Last.fm API uses two
// shapes: "@attr": {"nowplaying": "true"} for the currently playing
// track, and "date": {"uts": "1700000000"} for everything else. When
// neither is present the function returns an empty string and Render
// elides the played-at line.
func formatPlayedAt(t lastfmTrack) string {
	if t.Attr.NowPlaying == "true" {
		return "now playing"
	}
	if t.Date.UTS == "" {
		return ""
	}
	secs, err := strconv.ParseInt(t.Date.UTS, 10, 64)
	if err != nil {
		return ""
	}
	return time.Unix(secs, 0).UTC().Format("2006-01-02 15:04 UTC")
}

// largestImageURL picks the highest-resolution artwork URL Last.fm
// returned. The API orders images small -> medium -> large -> extralarge
// -> mega, so we iterate from the end and return the first non-empty
// "#text" value. Some tracks have empty URLs across all sizes; that
// returns "" and Render falls back to a placeholder rectangle.
func largestImageURL(images []lastfmImage) string {
	for i := len(images) - 1; i >= 0; i-- {
		if images[i].Text != "" {
			return images[i].Text
		}
	}
	return ""
}
