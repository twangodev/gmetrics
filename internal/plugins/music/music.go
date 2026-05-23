// Package music implements the gmetrics "music" plugin. The v1 scope is
// deliberately narrow: the only supported provider is Last.fm and the only
// supported mode is "recent" (recently played tracks). Other providers from
// upstream (Spotify, YouTube Music, Apple Music) and other modes (playlist,
// top) are out of scope; calling Fetch with a non-lastfm provider returns
// an error.
package music

import (
	"fmt"

	"github.com/twangodev/gmetrics/internal/plugin"
)

func init() {
	plugin.Register("music", func() plugin.Plugin { return &Plugin{} })
}

// Plugin is the gmetrics music plugin implementation.
type Plugin struct{}

// Name returns the plugin's stable identifier.
func (*Plugin) Name() string { return "music" }

// DecodeConfig parses the raw map produced by the YAML/env config loader
// into a typed Config. Missing keys fall back to defaultConfig() values.
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

// toString coerces a few common YAML-decoded scalar shapes into a string.
func toString(v any) (string, error) {
	switch s := v.(type) {
	case string:
		return s, nil
	default:
		return "", fmt.Errorf("want string, got %T", v)
	}
}

// toInt accepts int, int64, or float64 (YAML often decodes integers as
// float64 via interface{}) and returns the corresponding int.
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
