// Package people renders avatar grids for a user's followers and following.
package people

import (
	"fmt"

	"github.com/twangodev/gmetrics/internal/plugin"
)

func init() {
	plugin.Register("people", func() plugin.Plugin { return &Plugin{} })
}

type Plugin struct{}

func (*Plugin) Name() string { return "people" }

func (*Plugin) DecodeConfig(raw map[string]any) (any, error) {
	cfg := defaultConfig()
	if raw == nil {
		return cfg, nil
	}
	if v, ok := raw["types"]; ok {
		ts, err := toStringSlice(v)
		if err != nil {
			return nil, fmt.Errorf("people: types: %w", err)
		}
		cfg.Types = ts
	}
	if v, ok := raw["limit"]; ok {
		n, err := toInt(v)
		if err != nil {
			return nil, fmt.Errorf("people: limit: %w", err)
		}
		cfg.Limit = n
	}
	if v, ok := raw["size"]; ok {
		n, err := toInt(v)
		if err != nil {
			return nil, fmt.Errorf("people: size: %w", err)
		}
		cfg.Size = n
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
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
