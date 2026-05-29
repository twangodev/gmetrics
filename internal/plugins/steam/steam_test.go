package steam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/twangodev/gmetrics/internal/plugin"
)

// canned payloads for each endpoint. Returned verbatim by the test server
// so failures clearly identify which payload (and therefore which
// endpoint) the production code mis-decoded.
const (
	playerSummariesJSON = `{
		"response": {
			"players": [{
				"steamid": "76561197960287930",
				"personaname": "TestUser",
				"avatarfull": "https://media.example/avatar.jpg"
			}]
		}
	}`
	steamLevelJSON = `{"response": {"player_level": 42}}`
	ownedGamesJSON = `{
		"response": {
			"game_count": 2,
			"games": [
				{"appid": 730,  "name": "Counter-Strike", "playtime_forever": 12000, "img_icon_url": "abc",
				 "rtime_last_played": 1700000000,
				 "playtime_windows_forever": 2000, "playtime_linux_forever": 10000, "playtime_deck_forever": 9000},
				{"appid": 440,  "name": "Team Fortress 2", "playtime_forever":   600, "img_icon_url": "def"}
			]
		}
	}`
	recentGamesJSON = `{
		"response": {
			"games": [
				{"appid": 730, "name": "Counter-Strike", "playtime_2weeks": 180, "img_icon_url": "abc"}
			]
		}
	}`
	// 3 of 5 achievements unlocked; returned for whatever appid the stub is
	// asked about, which suffices for the single-game assertions below.
	playerAchievementsJSON = `{
		"playerstats": {
			"success": true,
			"achievements": [
				{"achieved": 1}, {"achieved": 1}, {"achieved": 1},
				{"achieved": 0}, {"achieved": 0}
			]
		}
	}`
)

// newStubServer stands up an httptest.Server that returns the canned JSON
// payload that matches each Steam API path. Any other path returns 404 so
// regressions in URL construction are loud.
func newStubServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ISteamUser/GetPlayerSummaries/v2/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(playerSummariesJSON))
	})
	mux.HandleFunc("/IPlayerService/GetSteamLevel/v1/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(steamLevelJSON))
	})
	mux.HandleFunc("/IPlayerService/GetOwnedGames/v1/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ownedGamesJSON))
	})
	mux.HandleFunc("/IPlayerService/GetRecentlyPlayedGames/v1/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(recentGamesJSON))
	})
	mux.HandleFunc("/ISteamUserStats/GetPlayerAchievements/v0001/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(playerAchievementsJSON))
	})
	return httptest.NewServer(mux)
}

// TestFetch_AllEndpoints_Mocked drives the fetch path against an
// httptest-served set of endpoints. It does not exercise the icon/avatar
// fetch path: env.HTTP is intentionally left nil so the only network
// activity is the four Steam API calls.
func TestFetch_AllEndpoints_Mocked(t *testing.T) {
	srv := newStubServer(t)
	t.Cleanup(srv.Close)

	cfg := Config{
		Token:            "test-token",
		User:             "76561197960287930",
		Sections:         []string{"player", "most-played", "recently-played"},
		RecentGamesLimit: 1,
		URL:              srv.URL,
	}
	require.NoError(t, cfg.validate())

	// Pass the httptest client to FetchWith so the test server's
	// self-signed/loopback wiring is honoured. env is constructed with
	// HTTP = nil, which the plugin uses as the signal to skip icon and
	// avatar fetches (those live on media.steampowered.com).
	env := &plugin.Env{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := FetchWith(ctx, srv.Client(), env, cfg)
	require.NoError(t, err)

	require.Equal(t, "TestUser", data.Player.Name)
	require.Equal(t, 42, data.Player.Level)
	require.Equal(t, 2, data.Player.TotalGames)
	// 12000 + 600 minutes = 12600 / 60 = 210 hours.
	require.InDelta(t, 210.0, data.Player.TotalHours, 0.001)
	require.Empty(t, data.Player.AvatarB64, "env.HTTP is nil so avatar must be skipped")

	require.NotEmpty(t, data.MostPlayed, "most-played should include at least one game")
	top := data.MostPlayed[0]
	require.Equal(t, "Counter-Strike", top.Name, "highest playtime wins")
	require.InDelta(t, 200.0, top.PlaytimeHours, 0.001)
	require.Empty(t, top.IconB64, "env.HTTP is nil so icons must be skipped")
	// 12000 / 12600 total minutes.
	require.InDelta(t, 0.952, top.PercentOfTotal, 0.01)
	// deck (9000) dominates desktop-linux (10000-9000) and windows (2000).
	require.Equal(t, "Steam Deck", top.Platform)
	require.Equal(t, "Nov 14, 2023", top.LastPlayed)
	require.True(t, top.HasAchievements)
	require.Equal(t, 3, top.AchUnlocked)
	require.Equal(t, 5, top.AchTotal)

	require.NotEmpty(t, data.Recently)
	require.Equal(t, "Counter-Strike", data.Recently[0].Name)
	require.InDelta(t, 3.0, data.Recently[0].PlaytimeHours, 0.001)
}

// TestRender_AllSections feeds a small, pre-built Data through Render and
// asserts the produced Fragment is non-empty with a height consistent with
// stacking all three section types.
func TestRender_AllSections(t *testing.T) {
	p := &Plugin{}
	data := Data{
		Sections: []string{"player", "most-played", "recently-played"},
		Player: Player{
			Name:       "TestUser",
			Level:      42,
			TotalGames: 12,
			TotalHours: 123.4,
		},
		MostPlayed: []Game{{
			AppID:         730,
			Name:          "Counter-Strike",
			PlaytimeHours: 200,
		}},
		Recently: []Game{{
			AppID:         730,
			Name:          "Counter-Strike",
			PlaytimeHours: 3,
		}},
	}
	frag, err := p.Render(nil, data)
	require.NoError(t, err)
	require.NotEmpty(t, frag.Body, "render should produce non-empty markup")
	require.Greater(t, frag.Height, 80, "three-section card should exceed 80 px tall")
	require.Equal(t, fragmentWidth, frag.Width)

	// Smoke-check the body contains markers from each section so a
	// regression that silently drops one is caught here rather than in
	// the integration tests downstream.
	require.True(t, strings.Contains(frag.Body, "steam-player"))
	require.True(t, strings.Contains(frag.Body, `data-section="most-played"`))
	require.True(t, strings.Contains(frag.Body, `data-section="recently-played"`))
}
