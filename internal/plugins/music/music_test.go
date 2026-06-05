package music_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/plugins/music"
)

func TestFetch_NonLastfmProviderErrorsMentioningLastfm(t *testing.T) {
	p := &music.Plugin{}
	cfg := music.Config{
		Provider: "spotify",
		Mode:     "recent",
		User:     "alice",
		Token:    "x",
		Limit:    4,
	}

	_, err := p.Fetch(context.Background(), &plugin.Env{}, cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lastfm")
}

func TestFetch_LastfmRecentTracks(t *testing.T) {
	const (
		nowPlayingTrackName = "Solar Sailer"
		datedTrackName      = "End of Line"
	)
	const datedTrackUTS = "1700000000" // 2023-11-14 22:13:20 UTC

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("user") != "alice" || q.Get("api_key") != "x" ||
			q.Get("format") != "json" || q.Get("limit") != "4" ||
			q.Get("method") != "user.getrecenttracks" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"recenttracks": map[string]any{
				"track": []map[string]any{
					{
						"name": nowPlayingTrackName,
						"artist": map[string]any{
							"#text": "Daft Punk",
						},
						"image": []map[string]any{
							{"#text": "https://img.invalid/sm.png", "size": "small"},
							{"#text": "https://img.invalid/lg.png", "size": "large"},
						},
						"@attr": map[string]any{"nowplaying": "true"},
					},
					{
						"name": datedTrackName,
						"artist": map[string]any{
							"#text": "Daft Punk",
						},
						"image": []map[string]any{
							{"#text": "", "size": "small"},
							{"#text": "https://img.invalid/eol.png", "size": "large"},
						},
						"date": map[string]any{
							"uts":   datedTrackUTS,
							"#text": "14 Nov 2023, 22:13",
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	cfg := music.Config{
		Provider: "lastfm",
		Mode:     "recent",
		Token:    "x",
		User:     "alice",
		Limit:    4,
		URL:      srv.URL,
	}

	p := &music.Plugin{}
	out, err := p.Fetch(context.Background(), &plugin.Env{HTTP: nil}, cfg)
	require.NoError(t, err)

	data, ok := out.(music.Data)
	require.True(t, ok, "Fetch must return music.Data, got %T", out)
	require.Equal(t, "lastfm", data.Provider)
	require.Equal(t, "recent", data.Mode)
	require.Len(t, data.Tracks, 2)

	require.Equal(t, nowPlayingTrackName, data.Tracks[0].Name)
	require.Equal(t, "Daft Punk", data.Tracks[0].Artist)
	require.Equal(t, "now playing", data.Tracks[0].PlayedAt)
	require.Empty(t, data.Tracks[0].ArtworkB64, "nil env.HTTP must skip artwork fetch")

	require.Equal(t, datedTrackName, data.Tracks[1].Name)
	require.Equal(t, "Daft Punk", data.Tracks[1].Artist)
	require.Equal(t, "2023-11-14 22:13 UTC", data.Tracks[1].PlayedAt)
	require.Empty(t, data.Tracks[1].ArtworkB64)
}

func TestRender_FourTracksTwoColumnGrid(t *testing.T) {
	const trackWithArtwork = "data:image/png;base64,AAAA"
	data := music.Data{
		Mode:     "recent",
		Provider: "lastfm",
		Tracks: []music.Track{
			{Name: "Track One", Artist: "Artist A"},
			{Name: "Track Two", Artist: "Artist B", ArtworkB64: trackWithArtwork},
			{Name: "Track Three", Artist: "Artist C"},
			{Name: "Track <four>", Artist: "Artist & D"},
		},
	}

	p := &music.Plugin{}
	frag, err := p.Render(nil, data)
	require.NoError(t, err)
	require.NotEmpty(t, frag.Body)
	require.Equal(t, 440, frag.Width)
	// 2 rows: header 48 + 2*40 + 8 trailing pad.
	require.Equal(t, 136, frag.Height)

	require.Contains(t, frag.Body, "<rect", "artworkless track renders a placeholder rect")
	require.Contains(t, frag.Body, "<image", "track with artwork renders an image")
	require.Equal(t, 4, strings.Count(frag.Body, `class="music-row"`))

	const minTextPaths = 10 // 2 header + (name, artist) per 4 rows
	require.GreaterOrEqual(t, strings.Count(frag.Body, "<path"), minTextPaths,
		"got %d <path> elements", strings.Count(frag.Body, "<path"))
	require.Equal(t, 2, strings.Count(frag.Body, "<svg"), "h2 and broadcast h3 octicons")
}

func TestRender_EightTracksFourRowGrid(t *testing.T) {
	tracks := make([]music.Track, 8)
	for i := range tracks {
		tracks[i] = music.Track{Name: "Track", Artist: "Artist"}
	}
	data := music.Data{Mode: "recent", Provider: "lastfm", Tracks: tracks}

	p := &music.Plugin{}
	frag, err := p.Render(nil, data)
	require.NoError(t, err)
	require.Equal(t, 440, frag.Width)
	// 4 rows: header 48 + 4*40 + 8 trailing pad.
	require.Equal(t, 216, frag.Height)
	require.Equal(t, 8, strings.Count(frag.Body, `class="music-row"`))
}

func TestPlugin_SelfRegistersAndDecodesConfig(t *testing.T) {
	p, ok := plugin.Lookup("music")
	require.True(t, ok, "music plugin must self-register via init()")
	require.Equal(t, "music", p.Name())

	raw, err := p.DecodeConfig(map[string]any{
		"provider": "lastfm",
		"mode":     "recent",
		"user":     "alice",
		"token":    "x",
		"limit":    4,
	})
	require.NoError(t, err)
	cfg, ok := raw.(music.Config)
	require.True(t, ok)
	require.Equal(t, "lastfm", cfg.Provider)
	require.Equal(t, "alice", cfg.User)
	require.Equal(t, 4, cfg.Limit)
}
