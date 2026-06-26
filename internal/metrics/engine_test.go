package metrics_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/twangodev/gmetrics/internal/config"
	"github.com/twangodev/gmetrics/internal/metrics"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/plugins/base"
)

type fakePlugin struct {
	name     string
	fetch    func(ctx context.Context, env *plugin.Env, cfg any) (any, error)
	renderFn func(env *plugin.Env, data any) (plugin.Fragment, error)
}

func (f fakePlugin) Name() string                             { return f.name }
func (f fakePlugin) DecodeConfig(map[string]any) (any, error) { return nil, nil }
func (f fakePlugin) Fetch(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
	return f.fetch(ctx, env, cfg)
}
func (f fakePlugin) Render(env *plugin.Env, data any) (plugin.Fragment, error) {
	return f.renderFn(env, data)
}

func registerFake(t *testing.T, name string, fp fakePlugin) {
	fp.name = name
	t.Cleanup(plugin.Snapshot())
	plugin.Register(name, func() plugin.Plugin { return fp })
}

func newTestEnv() *plugin.Env {
	return &plugin.Env{
		Login: "octocat",
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestEngine_RunsBaseFirstThenOthers(t *testing.T) {
	registerFake(t, "base", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			// Must be base.Data: the engine type-asserts it to populate Env.User.
			return base.Data{
				User: plugin.UserContext{Login: "octocat", Name: "The Octocat"},
			}, nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			return plugin.Fragment{Body: "<g id=\"base\"/>", Width: 480, Height: 200}, nil
		},
	})
	registerFake(t, "languages", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			require.Equal(t, "octocat", env.User.Login, "languages.Fetch should see Env.User populated by base")
			return "lang-marker", nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			require.Equal(t, "lang-marker", data)
			return plugin.Fragment{Body: "<g id=\"languages\"/>", Width: 440, Height: 120}, nil
		},
	})

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Languages: config.LanguagesConfig{Enabled: true},
		},
	}
	engine := &metrics.Engine{Env: newTestEnv()}
	frags, err := engine.Render(context.Background(), cfg)
	require.NoError(t, err)
	require.Len(t, frags, 2)
	require.Contains(t, frags[0].Body, "id=\"base\"")
	require.Contains(t, frags[1].Body, "id=\"languages\"")
}

func TestEngine_NonStrict_SubstitutesErrorFragment(t *testing.T) {
	registerFake(t, "base", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return base.Data{User: plugin.UserContext{Login: "octocat"}}, nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			return plugin.Fragment{Body: "<g id=\"base\"/>", Width: 480, Height: 200}, nil
		},
	})
	registerFake(t, "languages", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return nil, errors.New("boom: graphql exploded")
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			t.Fatalf("Render should not be called when Fetch errored")
			return plugin.Fragment{}, nil
		},
	})

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Languages: config.LanguagesConfig{Enabled: true},
		},
	}
	engine := &metrics.Engine{Env: newTestEnv(), Strict: false}
	frags, err := engine.Render(context.Background(), cfg)
	require.NoError(t, err)
	require.Len(t, frags, 2)
	require.Contains(t, frags[0].Body, "id=\"base\"")
	require.True(t, strings.Contains(frags[1].Body, "languages"),
		"error fragment body should mention plugin name; got %q", frags[1].Body)
	require.Contains(t, frags[1].Body, "plugin-error")
}

func okBase() fakePlugin {
	return fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return base.Data{User: plugin.UserContext{Login: "octocat"}}, nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			return plugin.Fragment{Body: "<g id=\"base\"/>", Width: 480, Height: 200}, nil
		},
	}
}

func okPeople() fakePlugin {
	return fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return "people-data", nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			return plugin.Fragment{Body: "<g id=\"people\"/>", Width: 440, Height: 80}, nil
		},
	}
}

func langsPeopleConfig() *config.Config {
	return &config.Config{
		Plugins: config.PluginsConfig{
			Languages: config.LanguagesConfig{Enabled: true},
			People:    config.PeopleConfig{Enabled: true},
		},
	}
}

func TestEngine_NonStrict_RecoversFetchPanic(t *testing.T) {
	registerFake(t, "base", okBase())
	registerFake(t, "languages", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			panic("assignment to entry in nil map")
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			t.Fatalf("Render must not run after Fetch panic")
			return plugin.Fragment{}, nil
		},
	})
	registerFake(t, "people", okPeople())

	engine := &metrics.Engine{Env: newTestEnv(), Strict: false}
	frags, err := engine.Render(context.Background(), langsPeopleConfig())
	require.NoError(t, err, "a recovered Fetch panic must not propagate in non-strict mode")
	require.Len(t, frags, 3)
	require.Contains(t, frags[1].Body, "plugin-error")
	require.Contains(t, frags[1].Body, "languages")
	require.Contains(t, frags[2].Body, "id=\"people\"", "sibling plugin must still render after a panic")
}

func TestEngine_NonStrict_RecoversRenderPanic(t *testing.T) {
	registerFake(t, "base", okBase())
	registerFake(t, "languages", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return "lang-ok", nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			panic("index out of range [3] with length 2")
		},
	})
	registerFake(t, "people", okPeople())

	engine := &metrics.Engine{Env: newTestEnv(), Strict: false}
	frags, err := engine.Render(context.Background(), langsPeopleConfig())
	require.NoError(t, err, "a recovered Render panic must not propagate in non-strict mode")
	require.Len(t, frags, 3)
	require.Contains(t, frags[1].Body, "plugin-error")
	require.Contains(t, frags[1].Body, "languages")
	require.Contains(t, frags[2].Body, "id=\"people\"", "sibling plugin must still render after a panic")
}

func TestEngine_Strict_FetchPanicAbortsCleanly(t *testing.T) {
	registerFake(t, "base", okBase())
	registerFake(t, "languages", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			panic("boom in fetch")
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			return plugin.Fragment{}, nil
		},
	})

	cfg := &config.Config{Plugins: config.PluginsConfig{Languages: config.LanguagesConfig{Enabled: true}}}
	engine := &metrics.Engine{Env: newTestEnv(), Strict: true}
	_, err := engine.Render(context.Background(), cfg)
	require.Error(t, err, "strict mode must return a clean error, not crash, on a Fetch panic")
	require.Contains(t, err.Error(), "languages")
	require.Contains(t, err.Error(), "panic")
}

func TestEngine_Strict_RenderPanicAbortsCleanly(t *testing.T) {
	registerFake(t, "base", okBase())
	registerFake(t, "languages", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return "lang-ok", nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			panic("boom in render")
		},
	})

	cfg := &config.Config{Plugins: config.PluginsConfig{Languages: config.LanguagesConfig{Enabled: true}}}
	engine := &metrics.Engine{Env: newTestEnv(), Strict: true}
	_, err := engine.Render(context.Background(), cfg)
	require.Error(t, err, "strict mode must return a clean error, not crash, on a Render panic")
	require.Contains(t, err.Error(), "languages")
	require.Contains(t, err.Error(), "panic")
}

func TestEngine_NonStrict_RecoversBasePanic(t *testing.T) {
	registerFake(t, "base", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			panic("base fetch exploded")
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			return plugin.Fragment{}, nil
		},
	})

	engine := &metrics.Engine{Env: newTestEnv(), Strict: false}
	frags, err := engine.Render(context.Background(), &config.Config{})
	require.NoError(t, err, "a recovered base panic must degrade, not propagate, in non-strict mode")
	require.Len(t, frags, 1)
	require.Contains(t, frags[0].Body, "plugin-error")
	require.Contains(t, frags[0].Body, "base")
}

func TestEngine_Strict_BasePanicAbortsCleanly(t *testing.T) {
	registerFake(t, "base", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			panic("base fetch exploded")
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			return plugin.Fragment{}, nil
		},
	})

	engine := &metrics.Engine{Env: newTestEnv(), Strict: true}
	_, err := engine.Render(context.Background(), &config.Config{})
	require.Error(t, err, "strict mode must return a clean error, not crash, on a base panic")
	require.Contains(t, err.Error(), "base")
	require.Contains(t, err.Error(), "panic")
}

func TestEngine_NonStrict_RecoversBaseRenderPanic(t *testing.T) {
	registerFake(t, "base", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return base.Data{User: plugin.UserContext{Login: "octocat"}}, nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			panic("base render exploded")
		},
	})

	engine := &metrics.Engine{Env: newTestEnv(), Strict: false}
	frags, err := engine.Render(context.Background(), &config.Config{})
	require.NoError(t, err, "a recovered base render panic must degrade, not propagate, in non-strict mode")
	require.Len(t, frags, 1)
	require.Contains(t, frags[0].Body, "plugin-error")
	require.Contains(t, frags[0].Body, "base")
}

func TestEngine_Strict_BaseRenderPanicAbortsCleanly(t *testing.T) {
	registerFake(t, "base", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return base.Data{User: plugin.UserContext{Login: "octocat"}}, nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			panic("base render exploded")
		},
	})

	engine := &metrics.Engine{Env: newTestEnv(), Strict: true}
	_, err := engine.Render(context.Background(), &config.Config{})
	require.Error(t, err, "strict mode must return a clean error, not crash, on a base render panic")
	require.Contains(t, err.Error(), "base")
	require.Contains(t, err.Error(), "panic")
}

func TestEngine_Strict_AbortsOnPluginError(t *testing.T) {
	registerFake(t, "base", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return base.Data{User: plugin.UserContext{Login: "octocat"}}, nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			return plugin.Fragment{Body: "<g id=\"base\"/>", Width: 480, Height: 200}, nil
		},
	})
	registerFake(t, "languages", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return nil, errors.New("boom: graphql exploded")
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			return plugin.Fragment{}, nil
		},
	})

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Languages: config.LanguagesConfig{Enabled: true},
		},
	}
	engine := &metrics.Engine{Env: newTestEnv(), Strict: true}
	_, err := engine.Render(context.Background(), cfg)
	require.Error(t, err)
}
