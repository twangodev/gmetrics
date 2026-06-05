// Package base renders the always-on header, activity, community, repositories and metadata card sections.
package base

import (
	"context"

	"github.com/twangodev/gmetrics/internal/plugin"
)

func init() {
	plugin.Register("base", func() plugin.Plugin { return Plugin{} })
}

type Plugin struct{}

func (Plugin) Name() string { return "base" }

// The engine builds a typed Config from the top-level tree; this only satisfies the interface.
func (Plugin) DecodeConfig(_ map[string]any) (any, error) {
	return Config{}, nil
}

func (p Plugin) Fetch(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
	c, ok := cfg.(Config)
	if !ok {
		c = defaultConfig()
	}
	return Fetch(ctx, env, c)
}

func (p Plugin) Render(env *plugin.Env, data any) (plugin.Fragment, error) {
	d, ok := data.(Data)
	if !ok {
		return plugin.Fragment{}, errInvalidData
	}
	return Render(env, d)
}
