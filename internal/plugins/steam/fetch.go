package steam

import (
	"context"
	"fmt"
	"net/http"
	"sort"

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
			AppID:         g.AppID,
			Name:          g.Name,
			IconB64:       fetchIcon(ctx, env, g.AppID, g.ImgIconURL),
			PlaytimeHours: float64(g.PlaytimeForever) / minutesPerHour,
		})
	}

	// Recently: take the API order verbatim (Steam already returns it
	// sorted by recency), trimmed to RecentGamesLimit.
	rlimit := cfg.RecentGamesLimit
	if rlimit <= 0 {
		rlimit = 1
	}
	if rlimit > len(recent.Response.Games) {
		rlimit = len(recent.Response.Games)
	}
	for i := 0; i < rlimit; i++ {
		g := recent.Response.Games[i]
		data.Recently = append(data.Recently, Game{
			AppID:         g.AppID,
			Name:          g.Name,
			IconB64:       fetchIcon(ctx, env, g.AppID, g.ImgIconURL),
			PlaytimeHours: float64(g.Playtime2Weeks) / minutesPerHour,
		})
	}

	return data, nil
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
