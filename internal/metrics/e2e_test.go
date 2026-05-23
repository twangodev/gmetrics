// E2E test: drive the whole engine pipeline against an httptest.Server that
// impersonates GitHub (REST + GraphQL), WakaTime, Last.fm and Steam. The test
// verifies the wiring end-to-end: configs flow through the engine, plugins
// fetch + render, the framer composes the SVG, and the result is byte-stable
// across runs (snapshot via goldie).
//
// Individual payload handling lives in each plugin's own _test.go; this file
// intentionally returns minimal canned responses, sized just to populate the
// plugin's Data without testing the plugin's response parsing in depth.
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

	// Re-import the real plugins so their init()s re-register them. This
	// matters because engine_test.go in the same package shadows the real
	// "base" and "languages" plugins with fakes in some tests; re-running
	// the real Register call here at test time restores them. We import
	// each plugin under a blank name purely for the init() side effect.
	basepkg "github.com/twangodev/gmetrics/internal/plugins/base"
	langpkg "github.com/twangodev/gmetrics/internal/plugins/languages"
	musicpkg "github.com/twangodev/gmetrics/internal/plugins/music"
	peoplepkg "github.com/twangodev/gmetrics/internal/plugins/people"
	steampkg "github.com/twangodev/gmetrics/internal/plugins/steam"
	wakapkg "github.com/twangodev/gmetrics/internal/plugins/wakatime"
)

// rewriteTransport sends every outgoing request to target.Host (preserving
// path + query), regardless of the request's original Host. We need this
// because the engine wires only some plugins' base-URLs from config
// (wakatime, music); steam and the avatar fetchers use hard-coded upstream
// hosts. With this transport the test server can be the single point of
// truth for every plugin's HTTP traffic.
type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Mutate a shallow clone so callers' Request isn't disturbed (matches
	// the http.RoundTripper contract).
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

// e2eHandler dispatches each incoming test-server request to the right canned
// response. Routing falls into four buckets:
//
//   - GraphQL: POST /graphql with a JSON body. The body's "query" field
//     contains a substring we use to pick the right payload (e.g. "followers",
//     "languages(first").
//   - WakaTime: GET /api/v1/users/<user>/stats/last_7_days
//   - Last.fm: GET /?method=user.getrecenttracks (root with method query)
//   - Steam:   GET /ISteamUser/... or /IPlayerService/...
//   - Avatars: any other GET path (returns a 1x1 PNG).
//
// Anything unmatched returns 404 so wiring regressions are loud.
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
			// Base plugin query — has many fields including
			// "contributionsCollection", which the other queries don't.
			_, _ = w.Write([]byte(graphqlBaseUserJSON))
		default:
			http.Error(w, "graphql: unrecognised query: "+bs, http.StatusBadRequest)
		}
	})

	// WakaTime stats endpoint.
	mux.HandleFunc("/api/v1/users/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(wakatimeStatsJSON))
	})

	// Steam endpoints.
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

	// REST rate_limit (optional; harmless if hit).
	mux.HandleFunc("/rate_limit", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(rateLimitJSON))
	})

	// Catch-all: dispatch by query (Last.fm hits the root with ?method=...)
	// and otherwise fall through to the avatar/icon stub. The avatar stub
	// returns a 1x1 transparent PNG so the people plugin can base64-encode
	// something without failing.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("method") == "user.getrecenttracks" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(lastfmRecentJSON))
			return
		}
		// Fallback: tiny PNG (1x1 transparent). Suitable for avatar /
		// artwork / icon fetches.
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(onePixelPNG)
	})

	return mux
}

// reRegisterRealPlugins reinstalls the real plugin constructors. engine_test.go
// in this same package overrides "base" and "languages" with fakes via
// plugin.Register; calling Register again with the real ctors restores them
// regardless of test ordering. We always do this in TestE2E to be safe.
func reRegisterRealPlugins() {
	plugin.Register("base", func() plugin.Plugin { return basepkg.Plugin{} })
	plugin.Register("languages", func() plugin.Plugin { return &langpkg.Plugin{} })
	plugin.Register("people", func() plugin.Plugin { return &peoplepkg.Plugin{} })
	plugin.Register("wakatime", func() plugin.Plugin { return wakapkg.Plugin{} })
	plugin.Register("music", func() plugin.Plugin { return &musicpkg.Plugin{} })
	plugin.Register("steam", func() plugin.Plugin { return &steampkg.Plugin{} })
}

// stripVolatile rewrites the SVG bytes to remove any content that drifts
// between runs (currently: nothing — the test config explicitly omits the
// metadata section so we don't render a timestamp — but the hook is in place
// for future additions like commit-hash watermarks).
func stripVolatile(s string) string {
	return s
}

func TestE2E_FullPipelineGolden(t *testing.T) {
	srv := httptest.NewServer(e2eHandler(t))
	t.Cleanup(srv.Close)

	target, err := url.Parse(srv.URL)
	require.NoError(t, err)

	// HTTP client that routes every outbound request to the test server.
	// Used both as env.HTTP (steam/wakatime/music/avatars) and as the
	// transport behind the GitHub client so plugin paths align.
	httpClient := &http.Client{
		Transport: &rewriteTransport{target: target, base: http.DefaultTransport},
	}

	// Build the GitHub clients pointed at the test server. REST + GraphQL
	// both end up at srv.URL via the rewrite transport, but we also set the
	// canonical base URLs explicitly so go-github composes correct paths.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clients, err := githubapi.New(ctx, githubapi.Config{
		Token:          "test-token",
		RESTBaseURL:    srv.URL + "/",
		GraphQLBaseURL: srv.URL + "/graphql",
		HTTPClient:     httpClient,
	})
	require.NoError(t, err)

	// Plugin Env. Note that env.HTTP is the rewriting client too: that's
	// what redirects wakatime/music/steam/avatar fetches to our stub.
	env := &plugin.Env{
		Login:   "alice",
		REST:    clients.REST,
		GraphQL: clients.GraphQL,
		HTTP:    httpClient,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// Mirror a simplified version of the user's actual workflow.
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

	// Note: music URL isn't wired through the engine, so we can't override
	// it via cfg directly. The music plugin's default base URL points at
	// ws.audioscrobbler.com; our rewriteTransport redirects that hostname
	// to the test server, so the request still lands on the Last.fm route
	// handler. Same for steam (api.steampowered.com) and avatar URLs from
	// the GitHub GraphQL response (we route everything via the test server).

	// Reset registrations to the real plugin ctors in case engine_test.go's
	// fake plugins ran earlier in the package and overwrote them.
	reRegisterRealPlugins()

	engine := &metrics.Engine{Env: env, Strict: false}
	frags, err := engine.Render(ctx, cfg)
	require.NoError(t, err)

	// We expect base + up to 5 optional plugins. Engine substitutes
	// ErrorFragments for any that failed gracefully, but the total count
	// should still be >= 4 (base + at least 3 of 5 optional plugins).
	if len(frags) < 4 {
		t.Fatalf("expected >= 4 fragments (base + optional plugins), got %d", len(frags))
	}

	// At least one fragment body should contain a glyph <path or <text
	// element, confirming rendering produced visible output.
	var sawGlyph bool
	for _, f := range frags {
		if strings.Contains(f.Body, "<path") || strings.Contains(f.Body, "<text") {
			sawGlyph = true
			break
		}
	}
	require.True(t, sawGlyph, "no fragment body contained <path or <text — rendering produced no visible output")

	// Compose the outer SVG. Width=480 matches the user's main card width.
	framer := render.NewFramer(render.Options{Width: 480})
	svg, err := framer.Compose(frags)
	require.NoError(t, err)

	// Sanity checks on the composed SVG.
	require.True(t,
		strings.HasPrefix(svg, `<svg xmlns="http://www.w3.org/2000/svg"`),
		"composed SVG should start with the expected root element; got prefix %q", svg[:min(80, len(svg))])
	require.Contains(t, svg, "<style>", "composed SVG should embed a <style> tag")
	require.Contains(t, svg, "--color-text", "composed SVG should reference the --color-text CSS variable")
	require.Greater(t, len(svg), 1024, "composed SVG byte count should exceed 1 KB (got %d)", len(svg))

	// Snapshot the composed SVG via goldie. Use a fixture dir under the
	// metrics package's testdata so `go test -update` writes to a stable
	// location independent of working directory.
	g := goldie.New(t,
		goldie.WithFixtureDir("testdata/golden"),
		goldie.WithNameSuffix(".golden.svg"),
	)
	g.Assert(t, "e2e_full", []byte(stripVolatile(svg)))
}

// --- Canned API payloads ---------------------------------------------------

// Base plugin GraphQL response. Sized just enough to populate UserContext +
// Activity + Community + Repositories without exercising the calendar/weeks
// path more than a couple of days.
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

// Languages plugin GraphQL response. One repo, two languages, non-fork so
// the bytes are aggregated.
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

// People plugin GraphQL responses. Both fetched per request type.
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

// WakaTime stats response.
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

// Last.fm user.getrecenttracks response.
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

// Steam canned responses.
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

// onePixelPNG is the smallest valid 1x1 transparent PNG. Avatar / artwork /
// icon fetches resolve to this so the plugins can succeed at the
// base64-encode step without us pulling a real image into the fixture set.
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

