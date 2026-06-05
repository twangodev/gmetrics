// Package music implements the gmetrics "music" plugin (Last.fm, recent tracks).
package music

import (
	"fmt"

	"github.com/twangodev/gmetrics/internal/plugin"
)

func init() {
	plugin.Register("music", func() plugin.Plugin { return &Plugin{} })
}

type Plugin struct{}

func (*Plugin) Name() string { return "music" }

func (*Plugin) DecodeConfig(raw map[string]any) (any, error) {
	cfg := defaultConfig()
	if raw == nil {
		return cfg, nil
	}
	if v, ok := raw["provider"]; ok {
		s, err := toString(v)
		if err != nil {
			return nil, fmt.Errorf("music: provider: %w", err)
		}
		cfg.Provider = s
	}
	if v, ok := raw["mode"]; ok {
		s, err := toString(v)
		if err != nil {
			return nil, fmt.Errorf("music: mode: %w", err)
		}
		cfg.Mode = s
	}
	if v, ok := raw["user"]; ok {
		s, err := toString(v)
		if err != nil {
			return nil, fmt.Errorf("music: user: %w", err)
		}
		cfg.User = s
	}
	if v, ok := raw["token"]; ok {
		s, err := toString(v)
		if err != nil {
			return nil, fmt.Errorf("music: token: %w", err)
		}
		cfg.Token = s
	}
	if v, ok := raw["url"]; ok {
		s, err := toString(v)
		if err != nil {
			return nil, fmt.Errorf("music: url: %w", err)
		}
		cfg.URL = s
	}
	if v, ok := raw["limit"]; ok {
		n, err := toInt(v)
		if err != nil {
			return nil, fmt.Errorf("music: limit: %w", err)
		}
		cfg.Limit = n
	}
	return cfg, nil
}

func toString(v any) (string, error) {
	switch s := v.(type) {
	case string:
		return s, nil
	default:
		return "", fmt.Errorf("want string, got %T", v)
	}
}

// YAML decodes integers as float64 through any, so accept all numeric shapes.
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
