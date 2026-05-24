// Package languages implements the gmetrics "languages" plugin. It produces
// the "Most used languages" stacked bar plus per-language legend that
// upstream's languages plugin renders in the classic template.
//
// v1 scope deliberately covers the user's actual workflow inputs:
//
//   - sections: [most-used]   (recently-used is out of scope)
//   - details:  [percentage]  (lines, bytes-size are out of scope)
//   - ignored:  [markdown]    (case-insensitive language name list)
//   - other:    false         (no "Other" bucket roll-up)
//   - limit:    8             (cap on legend rows)
//
// Plus the upstream-equivalent "indepth" toggle which, when true, switches
// from the cheap GraphQL aggregate (sizes returned by GitHub's Linguist
// pre-computed totals) to a per-repo shallow clone + go-enry walk. The
// indepth path is more expensive but produces byte totals computed from the
// user's actual code rather than GitHub's repository-wide totals.
package languages

import (
	"context"
	"fmt"

	"github.com/twangodev/gmetrics/internal/plugin"
)

func init() {
	plugin.Register("languages", func() plugin.Plugin { return &Plugin{} })
}

// Plugin is the gmetrics languages plugin implementation.
type Plugin struct{}

// Name returns the plugin's stable identifier.
func (*Plugin) Name() string { return "languages" }

// DecodeConfig parses the raw map produced by the YAML/env config loader
// into a typed Config. Missing keys fall back to defaultConfig().
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

// Fetch dispatches to the cheap GraphQL aggregate or the indepth clone+enry
// walk depending on cfg.Indepth.
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

// Render is implemented in render.go.
func (*Plugin) Render(env *plugin.Env, raw any) (plugin.Fragment, error) {
	data, ok := raw.(Data)
	if !ok {
		return plugin.Fragment{}, fmt.Errorf("languages: render: want Data, got %T", raw)
	}
	return renderFragment(env, data)
}

// toStringSlice accepts []string, []any (each entry stringable), or a single
// string (treated as a 1-element list). Matches the shape the YAML/env
// decoder may produce for a list-of-strings field.
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

// toBool accepts bool or the common stringy forms (true/false/yes/no/on/off
// /1/0) and returns the corresponding bool.
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
