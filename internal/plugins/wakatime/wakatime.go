// Package wakatime implements the "wakatime" gmetrics plugin. It fetches a
// user's WakaTime stats over a recent range and renders them as an SVG
// fragment matching the shape produced by upstream's wakatime partial.
package wakatime

import (
	"context"

	"github.com/twangodev/gmetrics/internal/plugin"
)

// Name is the stable identifier under which the plugin registers itself.
const Name = "wakatime"

// Plugin implements the gmetrics plugin.Plugin interface for WakaTime.
type Plugin struct{}

// Name returns the plugin's stable identifier.
func (Plugin) Name() string { return Name }

// DecodeConfig converts the raw YAML map for this plugin into a typed Config.
func (Plugin) DecodeConfig(raw map[string]any) (any, error) {
	return decodeConfig(raw)
}

// Fetch calls the WakaTime stats API and returns the trimmed Data result.
func (Plugin) Fetch(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
	c, ok := cfg.(Config)
	if !ok {
		c = defaultConfig()
	}
	return fetch(ctx, env, c)
}

// Render produces the SVG fragment from previously-fetched Data.
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
