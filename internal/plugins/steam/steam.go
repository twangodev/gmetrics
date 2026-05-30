// Package steam renders a Steam user's player summary and games.
package steam

import (
	"fmt"

	"github.com/twangodev/gmetrics/internal/plugin"
)

func init() {
	plugin.Register("steam", func() plugin.Plugin { return &Plugin{} })
}

type Plugin struct{}

func (*Plugin) Name() string { return "steam" }

func (*Plugin) DecodeConfig(raw map[string]any) (any, error) {
	cfg := defaultConfig()
	if raw == nil {
		return cfg, nil
	}
	if v, ok := raw["token"]; ok {
		s, err := toString(v)
		if err != nil {
			return nil, fmt.Errorf("steam: token: %w", err)
		}
		cfg.Token = s
	}
	if v, ok := raw["user"]; ok {
		s, err := toString(v)
		if err != nil {
			return nil, fmt.Errorf("steam: user: %w", err)
		}
		cfg.User = s
	}
	if v, ok := raw["sections"]; ok {
		ss, err := toStringSlice(v)
		if err != nil {
			return nil, fmt.Errorf("steam: sections: %w", err)
		}
		cfg.Sections = ss
	}
	if v, ok := raw["recent_games_limit"]; ok {
		n, err := toInt(v)
		if err != nil {
			return nil, fmt.Errorf("steam: recent_games_limit: %w", err)
		}
		cfg.RecentGamesLimit = n
	}
	if v, ok := raw["achievements_limit"]; ok {
		n, err := toInt(v)
		if err != nil {
			return nil, fmt.Errorf("steam: achievements_limit: %w", err)
		}
		cfg.AchievementsLimit = n
	}
	if v, ok := raw["url"]; ok {
		s, err := toString(v)
		if err != nil {
			return nil, fmt.Errorf("steam: url: %w", err)
		}
		cfg.URL = s
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func toString(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("want string, got %T", v)
	}
	return s, nil
}

func toStringSlice(v any) ([]string, error) {
	switch s := v.(type) {
	case []string:
		return s, nil
	case []any:
		out := make([]string, 0, len(s))
		for i, e := range s {
			str, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("element %d is %T, want string", i, e)
			}
			out = append(out, str)
		}
		return out, nil
	case string:
		return []string{s}, nil
	default:
		return nil, fmt.Errorf("want []string or string, got %T", v)
	}
}

// float64 case: YAML decodes integers as float64 through interface{}.
func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("want int, got %T", v)
	}
}
