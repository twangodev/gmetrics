// Package people implements the gmetrics "people" plugin. It renders one or
// more grids of avatars for the target user's followers and/or following
// lists. The v1 scope deliberately covers only the followers/following types
// from upstream's much larger people plugin; sponsors, contributors,
// stargazers, watchers and thanks are out of scope.
package people

import (
	"context"
	"fmt"

	"github.com/twangodev/gmetrics/internal/plugin"
)

func init() {
	plugin.Register("people", func() plugin.Plugin { return &Plugin{} })
}

// Plugin is the gmetrics people plugin implementation.
type Plugin struct{}

// Name returns the plugin's stable identifier.
func (*Plugin) Name() string { return "people" }

// DecodeConfig parses the raw map produced by the YAML/env config loader
// into a typed Config. Missing keys fall back to defaultConfig().
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

// Fetch is implemented in fetch.go.
// Render is implemented in render.go.

// toStringSlice accepts the common shapes a YAML/env decoder may produce
// for a list-of-strings field: []string, []any (each entry stringable), or
// a single string (treated as a 1-element list).
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

// Helpers shared between Fetch and the GraphQL paging code live in fetch.go.
// The Env type the plugin accepts is plugin.Env; declaring this var keeps
// the import in use even if Fetch is built without GraphQL paths exercised.
var _ context.Context = context.Background()
