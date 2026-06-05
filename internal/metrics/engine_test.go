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

func registerFake(name string, fp fakePlugin) {
	fp.name = name
	plugin.Register(name, func() plugin.Plugin { return fp })
}

func newTestEnv() *plugin.Env {
	return &plugin.Env{
		Login: "octocat",
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestEngine_RunsBaseFirstThenOthers(t *testing.T) {
	registerFake("base", fakePlugin{
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
	registerFake("languages", fakePlugin{
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
	registerFake("base", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return base.Data{User: plugin.UserContext{Login: "octocat"}}, nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			return plugin.Fragment{Body: "<g id=\"base\"/>", Width: 480, Height: 200}, nil
		},
	})
	registerFake("languages", fakePlugin{
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

func TestEngine_Strict_AbortsOnPluginError(t *testing.T) {
	registerFake("base", fakePlugin{
		fetch: func(ctx context.Context, env *plugin.Env, cfg any) (any, error) {
			return base.Data{User: plugin.UserContext{Login: "octocat"}}, nil
		},
		renderFn: func(env *plugin.Env, data any) (plugin.Fragment, error) {
			return plugin.Fragment{Body: "<g id=\"base\"/>", Width: 480, Height: 200}, nil
		},
	})
	registerFake("languages", fakePlugin{
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
