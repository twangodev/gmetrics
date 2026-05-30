package steam

import "fmt"

type Config struct {
	Token             string   `koanf:"token"`
	User              string   `koanf:"user"`
	Sections          []string `koanf:"sections"`
	RecentGamesLimit  int      `koanf:"recent_games_limit"`
	AchievementsLimit int      `koanf:"achievements_limit"`
	URL               string   `koanf:"url"`
}

func defaultConfig() Config {
	return Config{
		Sections:          []string{"player", "most-played", "recently-played"},
		RecentGamesLimit:  1,
		AchievementsLimit: 0,
	}
}

func (c Config) validate() error {
	if len(c.Sections) == 0 {
		return fmt.Errorf("steam: sections must not be empty")
	}
	for _, s := range c.Sections {
		switch s {
		case "player", "most-played", "recently-played":
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
