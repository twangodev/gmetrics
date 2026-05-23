// Package base implements the gmetrics "base" plugin. It renders the
// always-on header, activity, community, repositories and metadata
// sub-sections of the classic card layout. v1 deliberately mirrors a
// minimal subset of upstream's base plugin: deep-history activity,
// commit-search authoring, and packages listing are stretch goals.
package base

import (
	"context"

	"github.com/twangodev/gmetrics/internal/plugin"
)

func init() {
	plugin.Register("base", func() plugin.Plugin { return Plugin{} })
}

// Plugin is the gmetrics base plugin implementation. It is zero-sized so
// callers can construct it as a value or pointer without worrying about
// shared state.
type Plugin struct{}

// Name returns the plugin's stable identifier.
func (Plugin) Name() string { return "base" }

// DecodeConfig is a no-op for the base plugin: the engine constructs a
// typed Config directly from the top-level config tree (see
// internal/config.BaseConfig) and passes it as cfg in Fetch/Render. The
// method is required by the Plugin interface and returns an empty Config
// so the engine still has a non-nil value to thread through.
func (Plugin) DecodeConfig(_ map[string]any) (any, error) {
	return Config{}, nil
}

// Fetch is implemented in fetch.go.
func (p Plugin) Fetch(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
	c, ok := cfg.(Config)
	if !ok {
		c = defaultConfig()
	}
	return Fetch(ctx, env, c)
}

// Render is implemented in render.go.
func (p Plugin) Render(env *plugin.Env, data any) (plugin.Fragment, error) {
	d, ok := data.(Data)
	if !ok {
		return plugin.Fragment{}, errInvalidData
	}
	return Render(env, d)
}
