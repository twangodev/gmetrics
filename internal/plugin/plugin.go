package plugin

import (
	"context"
	"sort"
	"sync"
)

type Plugin interface {
	Name() string

	// Returning (nil, nil) is valid for plugins that have no configuration.
	DecodeConfig(raw map[string]any) (any, error)

	// The returned value is opaque to the engine and is passed back to Render.
	Fetch(ctx context.Context, env *Env, cfg any) (any, error)

	Render(env *Env, data any) (Fragment, error)
}

// Fragment is positioned at (0,0); the engine translates it into final position.
type Fragment struct {
	Body          string
	Width, Height int
}

var (
	regMu sync.RWMutex
	reg   = map[string]func() Plugin{}
)

// Register is safe to call concurrently from package init functions.
func Register(name string, ctor func() Plugin) {
	regMu.Lock()
	defer regMu.Unlock()
	reg[name] = ctor
}

func Lookup(name string) (Plugin, bool) {
	regMu.RLock()
	ctor, ok := reg[name]
	regMu.RUnlock()
	if !ok {
		return nil, false
	}
	return ctor(), true
}

// Snapshot captures the current registry and returns a function that restores
// it, so tests can register fakes and undo them via t.Cleanup.
func Snapshot() (restore func()) {
	regMu.Lock()
	defer regMu.Unlock()
	saved := make(map[string]func() Plugin, len(reg))
	for k, v := range reg {
		saved[k] = v
	}
	return func() {
		regMu.Lock()
		defer regMu.Unlock()
		reg = saved
	}
}

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
