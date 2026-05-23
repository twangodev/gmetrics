package plugin

import (
	"context"
	"sort"
	"sync"
)

// Plugin is the interface implemented by every gmetrics plugin. A plugin
// decodes its own configuration, fetches the data it needs from the
// environment, and renders an SVG fragment.
type Plugin interface {
	// Name returns the plugin's stable identifier (e.g. "languages").
	Name() string

	// DecodeConfig converts the raw map (typically from YAML) into the
	// plugin's typed configuration value. Returning (nil, nil) is valid for
	// plugins that have no configuration.
	DecodeConfig(raw map[string]any) (any, error)

	// Fetch performs any data acquisition the plugin needs. The returned
	// value is opaque to the engine and is passed back to Render.
	Fetch(ctx context.Context, env *Env, cfg any) (any, error)

	// Render produces a Fragment from the data previously returned by
	// Fetch.
	Render(env *Env, data any) (Fragment, error)
}

// Fragment is a positioned-at-(0,0) SVG fragment produced by a plugin. The
// engine is responsible for translating it into its final position in the
// containing document.
type Fragment struct {
	Body   string // SVG markup, typically a <g> with internal layout, positioned at (0,0)
	Width  int    // px, content width including the plugin's internal padding
	Height int    // px, content height including the plugin's internal padding
}

// Registry
var (
	regMu sync.RWMutex
	reg   = map[string]func() Plugin{}
)

// Register adds (or replaces) a plugin constructor under the given name.
// It is safe to call concurrently and is typically invoked from package
// init functions.
func Register(name string, ctor func() Plugin) {
	regMu.Lock()
	defer regMu.Unlock()
	reg[name] = ctor
}

// Lookup returns a freshly-constructed Plugin for the given name and a
// boolean indicating whether a constructor was registered.
func Lookup(name string) (Plugin, bool) {
	regMu.RLock()
	ctor, ok := reg[name]
	regMu.RUnlock()
	if !ok {
		return nil, false
	}
	return ctor(), true
}

// Names returns the sorted list of registered plugin names.
func Names() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(reg))
	for name := range reg {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
