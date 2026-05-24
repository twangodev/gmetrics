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

// TestFetch_RejectsNonLastfm verifies the v1 scope guard: any provider
// other than "lastfm" must produce a clear error mentioning the supported
// value. We don't care about the rest of the env here — the check fires
// before any network work.
func TestFetch_RejectsNonLastfm(t *testing.T) {
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
	require.Contains(t, err.Error(), "lastfm",
		"error message must mention the supported provider name")
}

// TestFetch_LastfmHappyPath drives the Last.fm path against a httptest
// server returning two canned tracks: one currently playing (no date
// object, has @attr.nowplaying) and one with a regular date.uts. env.HTTP
// is left nil so the plugin skips the artwork fetch and the assertions
// stay deterministic.
func TestFetch_LastfmHappyPath(t *testing.T) {
	const (
		track1Name = "Solar Sailer"
		track2Name = "End of Line"
	)
	const track2UTS = "1700000000" // 2023-11-14 22:13:20 UTC

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Confirm we passed the configured user/token/limit/format
		// through the URL builder.
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
						"name": track1Name,
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
						"name": track2Name,
						"artist": map[string]any{
							"#text": "Daft Punk",
						},
						"image": []map[string]any{
							{"#text": "", "size": "small"},
							{"#text": "https://img.invalid/eol.png", "size": "large"},
						},
						"date": map[string]any{
							"uts":   track2UTS,
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

	require.Equal(t, track1Name, data.Tracks[0].Name)
	require.Equal(t, "Daft Punk", data.Tracks[0].Artist)
	require.Equal(t, "now playing", data.Tracks[0].PlayedAt)
	require.Empty(t, data.Tracks[0].ArtworkB64, "artwork must be skipped with env.HTTP nil")

	require.Equal(t, track2Name, data.Tracks[1].Name)
	require.Equal(t, "Daft Punk", data.Tracks[1].Artist)
	require.Equal(t, "2023-11-14 22:13 UTC", data.Tracks[1].PlayedAt)
	require.Empty(t, data.Tracks[1].ArtworkB64)
}

// TestRender_FourTracks_NonEmpty exercises the rendering path with the
// canonical 4-track payload, including a track whose artwork is missing
// (placeholder branch) and a track with no PlayedAt (skipped <text>
// branch). We only assert the body is non-empty and the dimensions match
// the spec's formula; deeper layout assertions belong in golden tests.
func TestRender_FourTracks_NonEmpty(t *testing.T) {
	data := music.Data{
		Mode:     "recent",
		Provider: "lastfm",
		Tracks: []music.Track{
			{Name: "Track One", Artist: "Artist A", PlayedAt: "now playing"},
			{Name: "Track Two", Artist: "Artist B", PlayedAt: "2024-01-01 00:00 UTC", ArtworkB64: "data:image/png;base64,AAAA"},
			{Name: "Track Three", Artist: "Artist C", PlayedAt: ""},
			{Name: "Track <four>", Artist: "Artist & D", PlayedAt: "2024-02-02 02:02 UTC"},
		},
	}

	p := &music.Plugin{}
	frag, err := p.Render(nil, data)
	require.NoError(t, err)
	require.NotEmpty(t, frag.Body, "render body must be non-empty")
	require.Equal(t, 440, frag.Width)
	// New header layout: h2 (16px) baseline + h3 (12px) baseline below it,
	// totalling a 48px header block; followed by 4 track rows × 40px and
	// the spec's 8px trailing pad. 48 + 160 + 8 = 216.
	require.Equal(t, 216, frag.Height)

	// Header and track text are rendered as glyph <path> elements (text-as-path)
	// so we can't grep for the literal "Recently Played" / track-name strings.
	// Instead, assert the structural pieces we still emit verbatim.
	// The first track has no artwork URL -> placeholder rect.
	require.Contains(t, frag.Body, "<rect")
	// The second track has artwork -> <image>.
	require.Contains(t, frag.Body, "<image")
	// All four rows are emitted.
	require.Equal(t, 4, strings.Count(frag.Body, `class="music-row"`))
	// Text-as-path: two header lines (h2 + h3) plus three text lines per
	// row (name, artist, played-at), one of which is omitted for the third
	// track (PlayedAt empty). Total = 2 + 3 + 3 + 2 + 3 = 13 <path>.
	require.GreaterOrEqual(t, strings.Count(frag.Body, "<path"), 13,
		"expected at least 13 <path> elements (header + per-row text), got %d", strings.Count(frag.Body, "<path"))
	// The h2 octicon and h3 broadcast octicon are emitted as inline <svg>
	// elements; assert they're present so a regression that drops the icon
	// glyphs is caught here.
	require.Equal(t, 2, strings.Count(frag.Body, "<svg"),
		"expected 2 inline <svg> octicons (music-note h2, broadcast h3)")
}

// TestPlugin_RegisteredAndDecodes confirms the plugin self-registers via
// init() and that DecodeConfig wires up the common keys. This guards
// against silent breakage of the plugin contract.
func TestPlugin_RegisteredAndDecodes(t *testing.T) {
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
