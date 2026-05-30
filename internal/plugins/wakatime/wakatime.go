// Package wakatime renders a user's recent WakaTime stats as an SVG fragment.
package wakatime

import (
	"context"

	"github.com/twangodev/gmetrics/internal/plugin"
)

const Name = "wakatime"

type Plugin struct{}

func (Plugin) Name() string { return Name }

func (Plugin) DecodeConfig(raw map[string]any) (any, error) {
	return decodeConfig(raw)
}

func (Plugin) Fetch(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
	c, ok := cfg.(Config)
	if !ok {
		c = defaultConfig()
	}
	return fetch(ctx, env, c)
}

func (Plugin) Render(env *plugin.Env, data any) (plugin.Fragment, error) {
	d, ok := data.(Data)
	if !ok {
		return plugin.Fragment{}, nil
	}
	return renderFragment(env, d)
}

func init() {
	plugin.Register(Name, func() plugin.Plugin { return Plugin{} })
}
