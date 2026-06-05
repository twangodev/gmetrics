// Package languages renders the most-used languages stacked bar and legend.
package languages

import (
	"context"
	"fmt"

	"github.com/twangodev/gmetrics/internal/plugin"
)

func init() {
	plugin.Register("languages", func() plugin.Plugin { return &Plugin{} })
}

type Plugin struct{}

func (*Plugin) Name() string { return "languages" }

func (*Plugin) DecodeConfig(raw map[string]any) (any, error) {
	cfg := defaultConfig()
	if raw == nil {
		return cfg, nil
	}
	if v, ok := raw["sections"]; ok {
		ts, err := toStringSlice(v)
		if err != nil {
			return nil, fmt.Errorf("languages: sections: %w", err)
		}
		cfg.Sections = ts
	}
	if v, ok := raw["details"]; ok {
		ts, err := toStringSlice(v)
		if err != nil {
			return nil, fmt.Errorf("languages: details: %w", err)
		}
		cfg.Details = ts
	}
	if v, ok := raw["ignored"]; ok {
		ts, err := toStringSlice(v)
		if err != nil {
			return nil, fmt.Errorf("languages: ignored: %w", err)
		}
		cfg.Ignored = ts
	}
	if v, ok := raw["limit"]; ok {
		n, err := toInt(v)
		if err != nil {
			return nil, fmt.Errorf("languages: limit: %w", err)
		}
		cfg.Limit = n
	}
	if v, ok := raw["other"]; ok {
		b, err := toBool(v)
		if err != nil {
			return nil, fmt.Errorf("languages: other: %w", err)
		}
		cfg.Other = b
	}
	if v, ok := raw["indepth"]; ok {
		b, err := toBool(v)
		if err != nil {
			return nil, fmt.Errorf("languages: indepth: %w", err)
		}
		cfg.Indepth = b
	}
	if v, ok := raw["repo_batch"]; ok {
		n, err := toInt(v)
		if err != nil {
			return nil, fmt.Errorf("languages: repo_batch: %w", err)
		}
		cfg.RepoBatch = n
	}
	if v, ok := raw["repo_affiliations"]; ok {
		ts, err := toStringSlice(v)
		if err != nil {
			return nil, fmt.Errorf("languages: repo_affiliations: %w", err)
		}
		cfg.RepoAffiliations = ts
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (*Plugin) Fetch(ctx context.Context, env *plugin.Env, raw any) (any, error) {
	cfg, ok := raw.(Config)
	if !ok {
		cfg = defaultConfig()
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if env == nil {
		return nil, fmt.Errorf("languages: fetch: env is nil")
	}
	if cfg.Indepth {
		return fetchIndepth(ctx, env, cfg)
	}
	return fetchGraphQL(ctx, env, cfg)
}

func (*Plugin) Render(env *plugin.Env, raw any) (plugin.Fragment, error) {
	data, ok := raw.(Data)
	if !ok {
		return plugin.Fragment{}, fmt.Errorf("languages: render: want Data, got %T", raw)
	}
	return renderFragment(env, data)
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

func toBool(v any) (bool, error) {
	switch b := v.(type) {
	case bool:
		return b, nil
	case string:
		switch b {
		case "true", "yes", "on", "1":
			return true, nil
		case "false", "no", "off", "0", "":
			return false, nil
		}
		return false, fmt.Errorf("unrecognized bool string %q", b)
	default:
		return false, fmt.Errorf("want bool, got %T", v)
	}
}
