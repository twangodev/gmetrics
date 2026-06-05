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

type lastfmResponse struct {
	RecentTracks struct {
		Track []lastfmTrack `json:"track"`
	} `json:"recenttracks"`
}

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

type lastfmImage struct {
	Text string `json:"#text"`
	Size string `json:"size"`
}

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

	// Last.fm prepends the now-playing track on top of limit, so limit=8 can return 9.
	tracks := parsed.RecentTracks.Track
	if cfg.Limit > 0 && len(tracks) > cfg.Limit {
		tracks = tracks[:cfg.Limit]
	}

	data := Data{Mode: cfg.Mode, Provider: cfg.Provider}
	for _, raw := range tracks {
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

const (
	nowPlayingLabel = "now playing"
	playedAtLayout  = "2006-01-02 15:04 UTC"
)

func formatPlayedAt(t lastfmTrack) string {
	if t.Attr.NowPlaying == "true" {
		return nowPlayingLabel
	}
	if t.Date.UTS == "" {
		return ""
	}
	secs, err := strconv.ParseInt(t.Date.UTS, 10, 64)
	if err != nil {
		return ""
	}
	return time.Unix(secs, 0).UTC().Format(playedAtLayout)
}

// Last.fm orders images small -> mega, so the last non-empty URL is the largest.
func largestImageURL(images []lastfmImage) string {
	for i := len(images) - 1; i >= 0; i-- {
		if images[i].Text != "" {
			return images[i].Text
		}
	}
	return ""
}
