// Package metrics implements the orchestration engine that drives one card
// render: it runs the always-on base plugin first (so downstream plugins can
// rely on the populated UserContext), fans out the enabled optional plugins
// for parallel Fetch via errgroup, then renders each result sequentially in a
// deterministic layout order. Errors from optional plugins are either
// aborting (Strict mode) or replaced with a graceful error fragment.
package metrics

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/twangodev/gmetrics/internal/config"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/plugins/base"
	"github.com/twangodev/gmetrics/internal/plugins/languages"
	"github.com/twangodev/gmetrics/internal/plugins/music"
	"github.com/twangodev/gmetrics/internal/plugins/people"
	"github.com/twangodev/gmetrics/internal/plugins/steam"
	"github.com/twangodev/gmetrics/internal/plugins/wakatime"
)

// Engine orchestrates one card render: base first, then fan-out fetch + render
// for the enabled optional plugins.
type Engine struct {
	// Env is the runtime context handed to every plugin. The caller is
	// responsible for building this with authenticated REST/GraphQL/HTTP
	// clients, a logger, and the target user's Login. The engine populates
	// Env.User from the base plugin's output before invoking any other
	// plugin's Fetch.
	Env *plugin.Env
	// Strict, when true, causes any plugin error (in Fetch or Render) to
	// abort the entire render. When false, individual plugin errors are
	// substituted with a graceful error fragment so the rest of the card
	// still renders.
	Strict bool
}

// pluginRun pairs a registered plugin with its typed config so the parallel
// fetch loop can dispatch each plugin without re-deriving its config.
type pluginRun struct {
	name string
	p    plugin.Plugin
	cfg  any
}

// pluginResult is the per-plugin output of the parallel fetch phase. It is
// keyed by index into the runs slice so the deterministic render order can
// be restored regardless of completion order.
type pluginResult struct {
	name string
	data any
	err  error
}

// Render returns the fragments in display order: [base] followed by the
// enabled optional plugins in the canonical layout order
// (languages, people, wakatime, music, steam). In non-strict mode any
// fetch/render error is replaced with plugin.ErrorFragment so siblings still
// render. In strict mode the first error aborts and is returned to the caller.
func (e *Engine) Render(ctx context.Context, cfg *config.Config) ([]plugin.Fragment, error) {
	frags := make([]plugin.Fragment, 0, 6)

	// 1. Base plugin (sequential — populates Env.User used by other plugins).
	baseFrag, baseData, err := e.runBase(ctx, cfg)
	if err != nil {
		// Only reachable in strict mode; non-strict always returns a
		// graceful error fragment instead.
		return nil, err
	}
	frags = append(frags, baseFrag)

	// 2. Assemble the deterministic list of optional plugins to run.
	runs := assembleRuns(cfg, e.Env)

	// 3. Parallel fetch.
	results := make([]pluginResult, len(runs))
	g, gctx := errgroup.WithContext(ctx)
	for i, r := range runs {
		i, r := i, r
		g.Go(func() error {
			data, err := r.p.Fetch(gctx, e.Env, r.cfg)
			results[i] = pluginResult{name: r.name, data: data, err: err}
			if e.Strict && err != nil {
				return fmt.Errorf("plugin %s: %w", r.name, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		// Strict mode: any fetch error aborts. Soft-fail paths return nil
		// from the goroutine so this only fires when Strict is true.
		return nil, err
	}

	// 4. Render sequentially in runs order (preserves layout determinism).
	for i, r := range runs {
		res := results[i]
		if res.err != nil {
			// Strict mode already short-circuited above. Here we only reach
			// non-strict path: substitute an error fragment so siblings show.
			e.Env.Log.Error("plugin fetch failed", "plugin", r.name, "err", res.err)
			frags = append(frags, plugin.ErrorFragment(r.name, res.err))
			continue
		}
		frag, err := r.p.Render(e.Env, res.data)
		if err != nil {
			if e.Strict {
				return nil, fmt.Errorf("plugin %s render: %w", r.name, err)
			}
			e.Env.Log.Error("plugin render failed", "plugin", r.name, "err", err)
			frags = append(frags, plugin.ErrorFragment(r.name, err))
			continue
		}
		frags = append(frags, frag)
	}

	// Metadata footer: appended last so it sits below every plugin.
	if includesSection(cfg.Base.Sections, "metadata") && baseData != nil {
		if metaFrag, err := base.MetadataFragment(baseData); err == nil && metaFrag.Body != "" {
			frags = append(frags, metaFrag)
		} else if err != nil {
			e.Env.Log.Warn("metadata render failed", "err", err)
		}
	}

	return frags, nil
}

func includesSection(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// runBase fetches + renders the base plugin and (on success) populates
// Env.User from its output so downstream plugins can read it. In strict mode
// the first error short-circuits and is returned to the caller. In non-strict
// mode errors are logged and a graceful error fragment is substituted so the
// rest of the card still renders; the returned error is always nil in that
// path.
func (e *Engine) runBase(ctx context.Context, cfg *config.Config) (plugin.Fragment, any, error) {
	bp, ok := plugin.Lookup("base")
	if !ok {
		err := fmt.Errorf("base plugin not registered")
		if e.Strict {
			return plugin.Fragment{}, nil, err
		}
		e.Env.Log.Error("base plugin not registered")
		return plugin.ErrorFragment("base", err), nil, nil
	}

	baseCfg := base.Config{
		Sections:         cfg.Base.Sections,
		Hireable:         cfg.Base.Hireable,
		Indepth:          cfg.Base.Indepth,
		CommitsAuthoring: cfg.Base.CommitsAuthoring,
		Repos: base.RepoFetch{
			Affiliations: cfg.Base.Repositories.Affiliations,
			Max:          cfg.Base.Repositories.Max,
			Batch:        cfg.Base.Repositories.Batch,
			Forks:        cfg.Base.Repositories.Forks,
		},
	}

	data, err := bp.Fetch(ctx, e.Env, baseCfg)
	if err != nil {
		if e.Strict {
			return plugin.Fragment{}, nil, fmt.Errorf("plugin base: %w", err)
		}
		e.Env.Log.Error("base fetch failed", "err", err)
		return plugin.ErrorFragment("base", err), nil, nil
	}

	// Populate Env.User for downstream plugins. Per the locked API, the
	// base plugin guarantees its Fetch return value is a base.Data.
	if bd, ok := data.(base.Data); ok {
		e.Env.User = bd.User
	}

	frag, err := bp.Render(e.Env, data)
	if err != nil {
		if e.Strict {
			return plugin.Fragment{}, nil, fmt.Errorf("plugin base render: %w", err)
		}
		e.Env.Log.Error("base render failed", "err", err)
		return plugin.ErrorFragment("base", err), nil, nil
	}
	return frag, data, nil
}

// assembleRuns returns the deterministic, layout-ordered list of optional
// plugins to run for this render. Plugins disabled in cfg are skipped; a
// plugin that fails to look up (e.g. a typo in registration) is logged at
// warn level and dropped from the run set.
func assembleRuns(cfg *config.Config, env *plugin.Env) []pluginRun {
	runs := make([]pluginRun, 0, 5)

	if cfg.Plugins.Languages.Enabled {
		if p, ok := plugin.Lookup("languages"); ok {
			lc := cfg.Plugins.Languages
			cachePath := lc.IndepthCache
			if lc.Indepth && cachePath == "" {
				cachePath = ".gmetrics-cache/languages-indepth.json"
			}
			runs = append(runs, pluginRun{
				name: "languages",
				p:    p,
				cfg: languages.Config{
					Sections:         lc.Sections,
					Details:          lc.Details,
					Ignored:          lc.Ignored,
					Limit:            lc.Limit,
					Other:            lc.Other,
					Indepth:          lc.Indepth,
					IndepthCachePath: cachePath,
					RepoBatch:        cfg.Base.Repositories.Batch,
					RepoAffiliations: cfg.Base.Repositories.Affiliations,
					CommitsAuthoring: cfg.Base.CommitsAuthoring,
				},
			})
		} else {
			env.Log.Warn("plugin not registered", "plugin", "languages")
		}
	}

	if cfg.Plugins.People.Enabled {
		if p, ok := plugin.Lookup("people"); ok {
			pc := cfg.Plugins.People
			runs = append(runs, pluginRun{
				name: "people",
				p:    p,
				cfg: people.Config{
					Types: pc.Types,
					Limit: pc.Limit,
					Size:  pc.Size,
				},
			})
		} else {
			env.Log.Warn("plugin not registered", "plugin", "people")
		}
	}

	// Music renders before WakaTime to mirror upstream's sidebar layout
	// (Recently played → WakaTime → Steam).
	if cfg.Plugins.Music.Enabled {
		if p, ok := plugin.Lookup("music"); ok {
			mc := cfg.Plugins.Music
			runs = append(runs, pluginRun{
				name: "music",
				p:    p,
				cfg: music.Config{
					Provider: mc.Provider,
					Mode:     mc.Mode,
					User:     mc.User,
					Token:    mc.Token,
					Limit:    mc.Limit,
				},
			})
		} else {
			env.Log.Warn("plugin not registered", "plugin", "music")
		}
	}

	if cfg.Plugins.Wakatime.Enabled {
		if p, ok := plugin.Lookup("wakatime"); ok {
			wc := cfg.Plugins.Wakatime
			runs = append(runs, pluginRun{
				name: "wakatime",
				p:    p,
				cfg: wakatime.Config{
					Token:    wc.Token,
					URL:      wc.URL,
					User:     wc.User,
					Days:     wc.Days,
					Sections: wc.Sections,
					Limit:    wc.Limit,
				},
			})
		} else {
			env.Log.Warn("plugin not registered", "plugin", "wakatime")
		}
	}

	if cfg.Plugins.Steam.Enabled {
		if p, ok := plugin.Lookup("steam"); ok {
			sc := cfg.Plugins.Steam
			runs = append(runs, pluginRun{
				name: "steam",
				p:    p,
				cfg: steam.Config{
					Token:             sc.Token,
					User:              sc.User,
					Sections:          sc.Sections,
					RecentGamesLimit:  sc.RecentGamesLimit,
					AchievementsLimit: sc.AchievementsLimit,
				},
			})
		} else {
			env.Log.Warn("plugin not registered", "plugin", "steam")
		}
	}

	return runs
}
