package steam

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/twangodev/gmetrics/internal/img"
	"github.com/twangodev/gmetrics/internal/plugin"
)

// Steam reports all playtime values in minutes.
const minutesPerHour = 60.0

const defaultGamesLimit = 1

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

	if len(players.Response.Players) > 0 {
		p := players.Response.Players[0]
		data.Player.Name = p.PersonaName
		// Media lives on a separate host; tests skip it via env.HTTP=nil while still serving the API from httptest.
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

	// No separate games_limit in the workflow, so MostPlayed reuses RecentGamesLimit.
	byPlaytimeDesc := make([]ownedGame, len(owned.Response.Games))
	copy(byPlaytimeDesc, owned.Response.Games)
	sort.Slice(byPlaytimeDesc, func(i, j int) bool {
		return byPlaytimeDesc[i].PlaytimeForever > byPlaytimeDesc[j].PlaytimeForever
	})
	limit := cfg.RecentGamesLimit
	if limit <= 0 {
		limit = defaultGamesLimit
	}
	if limit > len(byPlaytimeDesc) {
		limit = len(byPlaytimeDesc)
	}
	for i := 0; i < limit; i++ {
		g := byPlaytimeDesc[i]
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

	// The recent endpoint omits rtime_last_played and lifetime playtime; join owned games by appid to recover them.
	ownedByID := make(map[int]ownedGame, len(owned.Response.Games))
	for _, g := range owned.Response.Games {
		ownedByID[g.AppID] = g
	}
	rlimit := cfg.RecentGamesLimit
	if rlimit <= 0 {
		rlimit = defaultGamesLimit
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

	enrichAchievements(ctx, hc, env, cfg, data.MostPlayed)
	enrichAchievements(ctx, hc, env, cfg, data.Recently)

	return data, nil
}

func shareOfTotal(mins, totalMins int) float64 {
	if totalMins <= 0 || mins <= 0 {
		return 0
	}
	if mins > totalMins {
		return 1
	}
	return float64(mins) / float64(totalMins)
}

// Absolute date (not "3 days ago") keeps golden tests deterministic and cached SVGs stable.
func formatLastPlayed(rtime int64) string {
	if rtime <= 0 {
		return ""
	}
	return time.Unix(rtime, 0).UTC().Format("Jan 2, 2006")
}

func dominantPlatform(windows, mac, linux, deck int) string {
	// Steam folds Deck time into the Linux total; subtract it to separate "Steam Deck" from desktop "Linux".
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

func enrichAchievements(ctx context.Context, hc *http.Client, env *plugin.Env, cfg Config, games []Game) {
	if hc == nil {
		return
	}
	var wg sync.WaitGroup
	for i := range games {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil && env != nil && env.Log != nil {
					env.Log.Error("steam: achievements panicked", "appid", games[i].AppID, "panic", r, "stack", string(debug.Stack()))
				}
			}()
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
