package music

import (
	"context"
	"fmt"

	"github.com/twangodev/gmetrics/internal/plugin"
)

func (*Plugin) Fetch(ctx context.Context, env *plugin.Env, raw any) (any, error) {
	cfg, ok := raw.(Config)
	if !ok {
		return nil, fmt.Errorf("music: fetch: want Config, got %T", raw)
	}
	if cfg.Provider != "lastfm" {
		return nil, fmt.Errorf("only music provider 'lastfm' is supported in v1; got %q", cfg.Provider)
	}
	if env == nil {
		return nil, fmt.Errorf("music: fetch: env is nil")
	}
	return fetchLastfm(ctx, env, cfg)
}
