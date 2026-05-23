package steam

import "fmt"

// Config is the typed configuration for the steam plugin. It mirrors the
// subset of upstream's steam plugin the user's v1 workflow exercises:
// player + most-played + recently-played, with achievements rendering
// disabled.
type Config struct {
	// Token is the Steam Web API key. Required for live API calls; tests
	// may leave it empty when URL points at a stub server that ignores it.
	Token string `koanf:"token"`
	// User is the 64-bit Steam ID of the target profile, formatted as a
	// base-10 string. Steam treats this as opaque, so we pass it through
	// to the API without any numeric coercion.
	User string `koanf:"user"`
	// Sections is the ordered list of sub-cards to render. Each value
	// must be one of "player", "most-played", "recently-played".
	Sections []string `koanf:"sections"`
	// RecentGamesLimit caps the number of recently-played games shown.
	// Also used as the cap for the most-played list, since upstream's
	// games_limit input is not in scope for v1.
	RecentGamesLimit int `koanf:"recent_games_limit"`
	// AchievementsLimit reserves space for future per-game achievements
	// rendering. v1 always treats this as 0 (no achievements rendered).
	AchievementsLimit int `koanf:"achievements_limit"`
	// URL, when non-empty, overrides the canonical Steam API base. Each
	// endpoint is then mapped as "<URL>/ISteamUser/GetPlayerSummaries/v2/"
	// (etc.) so tests can stand up a single httptest.Server and route
	// by request path.
	URL string `koanf:"url"`
}

// defaultConfig returns a Config seeded with the user's documented defaults.
// Token and User are intentionally empty; the engine is responsible for
// supplying them from secrets/env.
func defaultConfig() Config {
	return Config{
		Sections:          []string{"player", "most-played", "recently-played"},
		RecentGamesLimit:  1,
		AchievementsLimit: 0,
	}
}

// validate enforces the v1 invariants on a Config. It returns an error
// describing the first issue found.
func (c Config) validate() error {
	if len(c.Sections) == 0 {
		return fmt.Errorf("steam: sections must not be empty")
	}
	for _, s := range c.Sections {
		switch s {
		case "player", "most-played", "recently-played":
			// ok
		default:
			return fmt.Errorf("steam: unsupported section %q (v1 supports player, most-played, recently-played)", s)
		}
	}
	if c.RecentGamesLimit < 0 {
		return fmt.Errorf("steam: recent_games_limit must be >= 0")
	}
	if c.AchievementsLimit < 0 {
		return fmt.Errorf("steam: achievements_limit must be >= 0")
	}
	return nil
}
