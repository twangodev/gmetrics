package steam

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/twangodev/gmetrics/internal/img"
	"github.com/twangodev/gmetrics/internal/plugin"
)

// minutesPerHour is the conversion constant used throughout the steam
// plugin; Steam reports all playtime values in minutes.
const minutesPerHour = 60.0

// Fetch issues all required Steam API calls in parallel via an errgroup,
// then composes the rendered Data. When env.HTTP is nil (e.g. unit tests
// that only exercise rendering) no network calls happen and Fetch returns
// a descriptive error; callers driving Fetch directly should ensure they
// supply a *http.Client even if it just points at httptest.
func (*Plugin) Fetch(ctx context.Context, env *plugin.Env, raw any) (any, error) {
	cfg, ok := raw.(Config)
	if !ok {
		return nil, fmt.Errorf("steam: fetch: want Config, got %T", raw)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if env == nil {
		return nil, fmt.Errorf("steam: fetch: env is nil")
	}
	if env.HTTP == nil {
		return nil, fmt.Errorf("steam: fetch: env.HTTP is nil")
	}
	return FetchWith(ctx, env.HTTP, env, cfg)
}

// FetchWith runs the same fan-out as Fetch but accepts an explicit HTTP
// client so tests can drive it without constructing a full plugin.Env. The
// env argument is consulted only for the logger when avatar/icon fetches
// fail; pass nil to suppress warning logs.
func FetchWith(ctx context.Context, hc *http.Client, env *plugin.Env, cfg Config) (Data, error) {
	var (
		players playerSummariesResp
		level   steamLevelResp
		owned   ownedGamesResp
		recent  recentGamesResp
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		r, err := getPlayerSummary(gctx, hc, cfg)
		if err != nil {
			return fmt.Errorf("player summary: %w", err)
		}
		players = r
		return nil
	})
	g.Go(func() error {
		r, err := getLevel(gctx, hc, cfg)
		if err != nil {
			return fmt.Errorf("level: %w", err)
		}
		level = r
		return nil
	})
	g.Go(func() error {
		r, err := getOwnedGames(gctx, hc, cfg)
		if err != nil {
			return fmt.Errorf("owned games: %w", err)
		}
		owned = r
		return nil
	})
	g.Go(func() error {
		r, err := getRecent(gctx, hc, cfg)
		if err != nil {
			return fmt.Errorf("recent games: %w", err)
		}
		recent = r
		return nil
	})
	if err := g.Wait(); err != nil {
		return Data{}, fmt.Errorf("steam: fetch: %w", err)
	}

	data := Data{Sections: cfg.Sections}

	// Player. PersonaName/AvatarFull come from the first (only) player in
	// the summary response; level comes from its dedicated endpoint;
	// totals are derived from the owned-games payload.
	if len(players.Response.Players) > 0 {
		p := players.Response.Players[0]
		data.Player.Name = p.PersonaName
		// Avatar/icon fetches are deliberately gated on env.HTTP rather
		// than the API client: tests can stand up an httptest.Server for
		// the API calls while still passing env.HTTP=nil to skip the
		// (separately-hosted) media.steampowered.com lookups.
		if env != nil && env.HTTP != nil && p.AvatarFull != "" {
			b64, err := img.FetchAvatar(ctx, env.HTTP, p.AvatarFull)
			if err != nil {
				if env.Log != nil {
					env.Log.Warn("steam: avatar fetch failed", "err", err)
				}
			} else {
				data.Player.AvatarB64 = b64
			}
		}
	}
	data.Player.Level = level.Response.PlayerLevel
	data.Player.TotalGames = len(owned.Response.Games)
	var totalMins int
	for _, g := range owned.Response.Games {
		totalMins += g.PlaytimeForever
	}
	data.Player.TotalHours = float64(totalMins) / minutesPerHour

	// MostPlayed: top N by total playtime. The user's workflow does not
	// expose a separate games_limit, so we reuse RecentGamesLimit (default
	// 1). Sort a copy so we don't disturb the owned slice.
	mostPlayed := make([]ownedGame, len(owned.Response.Games))
	copy(mostPlayed, owned.Response.Games)
	sort.Slice(mostPlayed, func(i, j int) bool {
		return mostPlayed[i].PlaytimeForever > mostPlayed[j].PlaytimeForever
	})
	limit := cfg.RecentGamesLimit
	if limit <= 0 {
		limit = 1
	}
	if limit > len(mostPlayed) {
		limit = len(mostPlayed)
	}
	for i := 0; i < limit; i++ {
		g := mostPlayed[i]
		data.MostPlayed = append(data.MostPlayed, Game{
			AppID:          g.AppID,
			Name:           g.Name,
			IconB64:        fetchIcon(ctx, env, g.AppID, g.ImgIconURL),
			PlaytimeHours:  float64(g.PlaytimeForever) / minutesPerHour,
			LastPlayed:     formatLastPlayed(g.RtimeLastPlayed),
			PercentOfTotal: shareOfTotal(g.PlaytimeForever, totalMins),
			Platform:       dominantPlatform(g.PlaytimeWindows, g.PlaytimeMac, g.PlaytimeLinux, g.PlaytimeDeck),
		})
	}

	// Recently: take the API order verbatim (Steam already returns it
	// sorted by recency), trimmed to RecentGamesLimit. The recent endpoint
	// omits rtime_last_played, so join the owned-games list by appid to
	// recover a last-played date and lifetime playtime for the % stat.
	ownedByID := make(map[int]ownedGame, len(owned.Response.Games))
	for _, g := range owned.Response.Games {
		ownedByID[g.AppID] = g
	}
	rlimit := cfg.RecentGamesLimit
	if rlimit <= 0 {
		rlimit = 1
	}
	if rlimit > len(recent.Response.Games) {
		rlimit = len(recent.Response.Games)
	}
	for i := 0; i < rlimit; i++ {
		g := recent.Response.Games[i]
		game := Game{
			AppID:         g.AppID,
			Name:          g.Name,
			IconB64:       fetchIcon(ctx, env, g.AppID, g.ImgIconURL),
			PlaytimeHours: float64(g.Playtime2Weeks) / minutesPerHour,
			Platform:      dominantPlatform(g.PlaytimeWindows, g.PlaytimeMac, g.PlaytimeLinux, g.PlaytimeDeck),
		}
		forever := g.PlaytimeForever
		if o, ok := ownedByID[g.AppID]; ok {
			game.LastPlayed = formatLastPlayed(o.RtimeLastPlayed)
			if forever == 0 {
				forever = o.PlaytimeForever
			}
		}
		game.PercentOfTotal = shareOfTotal(forever, totalMins)
		data.Recently = append(data.Recently, game)
	}

	// Achievement counts need one extra call per displayed game; do them
	// concurrently and best-effort (a private profile or a game without
	// achievements simply leaves the stat unset).
	enrichAchievements(ctx, hc, env, cfg, data.MostPlayed)
	enrichAchievements(ctx, hc, env, cfg, data.Recently)

	return data, nil
}

// shareOfTotal returns mins/totalMins clamped to [0,1], or 0 when the total
// is unknown.
func shareOfTotal(mins, totalMins int) float64 {
	if totalMins <= 0 || mins <= 0 {
		return 0
	}
	if mins > totalMins {
		return 1
	}
	return float64(mins) / float64(totalMins)
}

// formatLastPlayed renders a Unix timestamp as an absolute date. An absolute
// date (rather than "3 days ago") keeps the rendered card deterministic for
// golden tests and stable in a cached SVG.
func formatLastPlayed(rtime int64) string {
	if rtime <= 0 {
		return ""
	}
	return time.Unix(rtime, 0).UTC().Format("Jan 2, 2006")
}

// dominantPlatform returns the label of the most-played platform given the
// per-platform minute counts, or "" when Steam reports no breakdown. Deck
// time is a subset of the Linux total, so it's subtracted out to separate
// "Steam Deck" from desktop "Linux".
func dominantPlatform(windows, mac, linux, deck int) string {
	desktopLinux := linux - deck
	if desktopLinux < 0 {
		desktopLinux = 0
	}
	best, bestMins := "", 0
	for _, c := range []struct {
		name string
		mins int
	}{
		{"Windows", windows},
		{"macOS", mac},
		{"Linux", desktopLinux},
		{"Steam Deck", deck},
	} {
		if c.mins > bestMins {
			best, bestMins = c.name, c.mins
		}
	}
	return best
}

// enrichAchievements fills the achievement counts on each game in place,
// fetching concurrently. hc is the Steam API client; when nil (render-only
// paths) the function is a no-op. Per-game failures are logged at debug and
// otherwise ignored.
func enrichAchievements(ctx context.Context, hc *http.Client, env *plugin.Env, cfg Config, games []Game) {
	if hc == nil {
		return
	}
	var wg sync.WaitGroup
	for i := range games {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			unlocked, total, err := getAchievements(ctx, hc, cfg, games[i].AppID)
			if err != nil {
				if env != nil && env.Log != nil {
					env.Log.Debug("steam: achievements unavailable", "appid", games[i].AppID, "err", err)
				}
				return
			}
			if total > 0 {
				games[i].HasAchievements = true
				games[i].AchUnlocked = unlocked
				games[i].AchTotal = total
			}
		}(i)
	}
	wg.Wait()
}

// fetchIcon returns a data: URL for a game icon, or the empty string when
// env.HTTP is unavailable, the icon hash is empty, or the fetch failed.
// Errors are logged via env.Log when present so they show up in the
// developer's stream without aborting the render.
func fetchIcon(ctx context.Context, env *plugin.Env, appid int, hash string) string {
	if env == nil || env.HTTP == nil || hash == "" {
		return ""
	}
	u := iconURL(appid, hash)
	if u == "" {
		return ""
	}
	b64, err := img.FetchAvatar(ctx, env.HTTP, u)
	if err != nil {
		if env.Log != nil {
			env.Log.Warn("steam: icon fetch failed", "appid", appid, "err", err)
		}
		return ""
	}
	return b64
}
