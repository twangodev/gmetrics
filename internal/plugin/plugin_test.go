package plugin_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twangodev/gmetrics/internal/plugin"
)

type fake struct{}

func (fake) Name() string                                         { return "fake" }
func (fake) DecodeConfig(map[string]any) (any, error)             { return nil, nil }
func (fake) Fetch(context.Context, *plugin.Env, any) (any, error) { return "data", nil }
func (fake) Render(*plugin.Env, any) (plugin.Fragment, error) {
	return plugin.Fragment{Body: "<g/>", Width: 100, Height: 50}, nil
}

func TestRegistry_RegisterLookup(t *testing.T) {
	plugin.Register("fake", func() plugin.Plugin { return fake{} })
	p, ok := plugin.Lookup("fake")
	require.True(t, ok)
	require.Equal(t, "fake", p.Name())
}

func TestRegistry_LookupMissing(t *testing.T) {
	_, ok := plugin.Lookup("not-a-plugin")
	require.False(t, ok)
}

func TestRegistry_Names_ContainsRegistered(t *testing.T) {
	plugin.Register("foo", func() plugin.Plugin { return fake{} })
	names := plugin.Names()
	found := false
	for _, n := range names {
		if n == "foo" {
			found = true
		}
	}
	require.True(t, found)
}
