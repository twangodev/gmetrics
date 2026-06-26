package metrics_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/require"

	"github.com/twangodev/gmetrics/internal/config"
	"github.com/twangodev/gmetrics/internal/githubapi"
	"github.com/twangodev/gmetrics/internal/metrics"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/render"
)

// rewriteTransport redirects every request to target.Host (keeping path+query)
// so the test server is the single upstream for plugins with hard-coded hosts.
type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// RoundTripper must not mutate the caller's request; mutate a clone.
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.target.Scheme
	req.URL.Host = rt.target.Host
	req.Host = rt.target.Host
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func e2eHandler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bs := string(body)

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(bs, "followers(first"):
			_, _ = w.Write([]byte(graphqlFollowersJSON))
		case strings.Contains(bs, "following(first"):
			_, _ = w.Write([]byte(graphqlFollowingJSON))
		case strings.Contains(bs, "languages(first"):
			_, _ = w.Write([]byte(graphqlLanguagesJSON))
		case strings.Contains(bs, "contributionsCollection"):
			_, _ = w.Write([]byte(graphqlBaseUserJSON))
		default:
			http.Error(w, "graphql: unrecognised query: "+bs, http.StatusBadRequest)
		}
	})

	mux.HandleFunc("/api/v1/users/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(wakatimeStatsJSON))
	})

	mux.HandleFunc("/ISteamUser/GetPlayerSummaries/v2/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(steamPlayerSummariesJSON))
	})
	mux.HandleFunc("/IPlayerService/GetSteamLevel/v1/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(steamLevelJSON))
	})
	mux.HandleFunc("/IPlayerService/GetOwnedGames/v1/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(steamOwnedGamesJSON))
	})
	mux.HandleFunc("/IPlayerService/GetRecentlyPlayedGames/v1/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(steamRecentJSON))
	})

	mux.HandleFunc("/rate_limit", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(rateLimitJSON))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("method") == "user.getrecenttracks" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(lastfmRecentJSON))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(onePixelPNG)
	})

	return mux
}

// stripVolatile is the hook for redacting run-to-run drift; nothing drifts yet.
func stripVolatile(s string) string {
	return s
}

func TestE2E_FullPipelineGolden(t *testing.T) {
	srv := httptest.NewServer(e2eHandler(t))
	t.Cleanup(srv.Close)

	target, err := url.Parse(srv.URL)
	require.NoError(t, err)

	routingClient := &http.Client{
		Transport: &rewriteTransport{target: target, base: http.DefaultTransport},
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clients, err := githubapi.New(ctx, githubapi.Config{
		Token:          "test-token",
		RESTBaseURL:    srv.URL + "/",
		GraphQLBaseURL: srv.URL + "/graphql",
		HTTPClient:     routingClient,
	})
	require.NoError(t, err)

	env := &plugin.Env{
		Login:   "alice",
		REST:    clients.REST,
		GraphQL: clients.GraphQL,
		HTTP:    routingClient,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	cfg := &config.Config{
		User:     "alice",
		Filename: "/tmp/e2e.svg",
		GitHub:   config.GitHubConfig{Token: "test-token"},
		Base: config.BaseConfig{
			Sections: []string{"header", "activity"},
			Repositories: config.RepoFetch{
				Affiliations: []string{"owner"},
				Max:          100,
				Batch:        100,
				Forks:        false,
			},
		},
		Plugins: config.PluginsConfig{
			Languages: config.LanguagesConfig{
				Enabled:  true,
				Sections: []string{"most-used"},
				Limit:    8,
			},
			People: config.PeopleConfig{
				Enabled: true,
				Types:   []string{"followers", "following"},
				Limit:   4,
				Size:    28,
			},
			Wakatime: config.WakatimeConfig{
				Enabled:  true,
				URL:      srv.URL,
				Token:    "wkt",
				User:     "alice",
				Days:     7,
				Sections: []string{"time", "projects-graphs"},
				Limit:    2,
			},
			Music: config.MusicConfig{
				Enabled:  true,
				Provider: "lastfm",
				Mode:     "recent",
				User:     "alice",
				Token:    "lfm",
				Limit:    2,
			},
			Steam: config.SteamConfig{
				Enabled:          true,
				Token:            "stm",
				User:             "76561198000000000",
				Sections:         []string{"player", "most-played"},
				RecentGamesLimit: 1,
			},
		},
		Output: config.OutputConfig{Action: "none"},
	}

	engine := &metrics.Engine{Env: env, Strict: false}
	frags, err := engine.Render(ctx, cfg)
	require.NoError(t, err)

	const minFragments = 4 // base + at least 3 of 5 optional plugins (failures render as ErrorFragments)
	if len(frags) < minFragments {
		t.Fatalf("expected >= %d fragments (base + optional plugins), got %d", minFragments, len(frags))
	}

	var sawGlyph bool
	for _, f := range frags {
		if strings.Contains(f.Body, "<path") || strings.Contains(f.Body, "<text") {
			sawGlyph = true
			break
		}
	}
	require.True(t, sawGlyph, "no fragment body contained <path or <text — rendering produced no visible output")

	const mainCardWidth = 480
	framer := render.NewFramer(render.Options{Width: mainCardWidth})
	svg, err := framer.Compose(frags)
	require.NoError(t, err)

	require.True(t,
		strings.HasPrefix(svg, `<svg xmlns="http://www.w3.org/2000/svg"`),
		"composed SVG should start with the expected root element; got prefix %q", svg[:min(80, len(svg))])
	require.Contains(t, svg, "<style>", "composed SVG should embed a <style> tag")
	require.Contains(t, svg, ".text-h1", "composed SVG should include the h1 theme class")
	require.Greater(t, len(svg), 1024, "composed SVG byte count should exceed 1 KB (got %d)", len(svg))

	g := goldie.New(t,
		goldie.WithFixtureDir("testdata/golden"),
		goldie.WithNameSuffix(".golden.svg"),
	)
	g.Assert(t, "e2e_full", []byte(stripVolatile(svg)))
}

const graphqlBaseUserJSON = `{
  "data": {
    "user": {
      "login": "alice",
      "name": "Alice A.",
      "avatarUrl": "/avatar/alice.png",
      "bio": "Hello from the test fixture",
      "createdAt": "2018-01-01T00:00:00Z",
      "databaseId": 12345,
      "followers": {"totalCount": 12},
      "following": {"totalCount": 7},
      "issueComments": {"totalCount": 9},
      "organizations": {"totalCount": 2},
      "starredRepositories": {"totalCount": 30},
      "watching": {"totalCount": 5},
      "sponsorshipsAsSponsor": {"totalCount": 1},
      "repositories": {"totalCount": 14, "totalDiskUsage": 1024},
      "contributionsCollection": {
        "totalCommitContributions": 200,
        "totalPullRequestContributions": 25,
        "totalPullRequestReviewContributions": 8,
        "totalIssueContributions": 12,
        "contributionCalendar": {
          "weeks": [
            {"contributionDays": [
              {"contributionCount": 3, "date": "2026-05-15"},
              {"contributionCount": 5, "date": "2026-05-16"}
            ]}
          ]
        }
      }
    }
  }
}`

const graphqlLanguagesJSON = `{
  "data": {
    "user": {
      "repositories": {
        "nodes": [
          {
            "nameWithOwner": "alice/hello",
            "isFork": false,
            "languages": {
              "edges": [
                {"size": 4096, "node": {"name": "Go", "color": "#00ADD8"}},
                {"size": 1024, "node": {"name": "Python", "color": "#3572A5"}}
              ]
            }
          }
        ],
        "pageInfo": {"endCursor": null, "hasNextPage": false}
      }
    }
  }
}`

const graphqlFollowersJSON = `{
  "data": {
    "user": {
      "followers": {
        "nodes": [
          {"login": "bob",   "avatarUrl": "/avatar/bob.png"},
          {"login": "carol", "avatarUrl": "/avatar/carol.png"}
        ]
      }
    }
  }
}`

const graphqlFollowingJSON = `{
  "data": {
    "user": {
      "following": {
        "nodes": [
          {"login": "dave",  "avatarUrl": "/avatar/dave.png"},
          {"login": "eve",   "avatarUrl": "/avatar/eve.png"}
        ]
      }
    }
  }
}`

const wakatimeStatsJSON = `{
  "data": {
    "total_seconds": 36000,
    "daily_average": 5142.85,
    "projects":  [
      {"name": "gmetrics", "percent": 60.0, "total_seconds": 21600},
      {"name": "side-app", "percent": 40.0, "total_seconds": 14400}
    ],
    "languages": [
      {"name": "Go",     "percent": 75.0, "total_seconds": 27000},
      {"name": "Python", "percent": 25.0, "total_seconds":  9000}
    ],
    "editors": [
      {"name": "GoLand", "percent": 100.0, "total_seconds": 36000}
    ],
    "operating_systems": [
      {"name": "Linux",  "percent": 100.0, "total_seconds": 36000}
    ]
  }
}`

const lastfmRecentJSON = `{
  "recenttracks": {
    "track": [
      {
        "name": "Test Track",
        "artist": {"#text": "Test Artist"},
        "image": [
          {"#text": "/img/lf-small.png",  "size": "small"},
          {"#text": "/img/lf-medium.png", "size": "medium"}
        ],
        "date": {"uts": "1700000000"}
      },
      {
        "name": "Now Playing Song",
        "artist": {"#text": "Live Artist"},
        "image": [
          {"#text": "/img/lf-small.png", "size": "small"}
        ],
        "@attr": {"nowplaying": "true"}
      }
    ]
  }
}`

const steamPlayerSummariesJSON = `{
  "response": {
    "players": [
      {"steamid": "76561198000000000", "personaname": "Alice Steam", "avatarfull": "/img/steam-avatar.png"}
    ]
  }
}`
const steamLevelJSON = `{"response": {"player_level": 42}}`
const steamOwnedGamesJSON = `{
  "response": {
    "game_count": 2,
    "games": [
      {"appid": 730, "name": "Counter-Strike", "playtime_forever": 6000, "img_icon_url": "abc"},
      {"appid": 570, "name": "Dota 2",         "playtime_forever": 1200, "img_icon_url": "def"}
    ]
  }
}`
const steamRecentJSON = `{
  "response": {
    "games": [
      {"appid": 730, "name": "Counter-Strike", "playtime_2weeks": 180, "img_icon_url": "abc"}
    ]
  }
}`

const rateLimitJSON = `{"resources":{"core":{"limit":5000,"remaining":5000,"reset":0},"graphql":{"limit":5000,"remaining":5000,"reset":0},"search":{"limit":30,"remaining":30,"reset":0}},"rate":{"limit":5000,"remaining":5000,"reset":0}}`

// onePixelPNG is a valid 1x1 transparent PNG so avatar/artwork fetches base64-encode without a real image.
var onePixelPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}
