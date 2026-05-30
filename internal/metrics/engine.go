// Package metrics orchestrates one card render: base plugin first, then a
// parallel fetch and deterministic render of the enabled optional plugins.
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

type Engine struct {
	Env *plugin.Env
	// Strict aborts the whole render on any plugin error; otherwise each
	// failing plugin is replaced with a graceful error fragment.
	Strict bool
}

type pluginRun struct {
	name string
	p    plugin.Plugin
	cfg  any
}

type pluginResult struct {
	name string
	data any
	err  error
}

func (e *Engine) Render(ctx context.Context, cfg *config.Config) ([]plugin.Fragment, error) {
	frags := make([]plugin.Fragment, 0, 6)

	baseFrag, baseData, err := e.runBase(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if baseFrag.Body != "" {
		frags = append(frags, baseFrag)
	}

	runs := assembleRuns(cfg, e.Env)

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
		return nil, err
	}

	for i, r := range runs {
		res := results[i]
		if res.err != nil {
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

// runBase populates Env.User from the base output so downstream plugins can read it.
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

const defaultLanguagesIndepthCachePath = ".gmetrics-cache/languages-indepth.json"

func assembleRuns(cfg *config.Config, env *plugin.Env) []pluginRun {
	runs := make([]pluginRun, 0, 5)

	if cfg.Plugins.Languages.Enabled {
		if p, ok := plugin.Lookup("languages"); ok {
			lc := cfg.Plugins.Languages
			cachePath := lc.IndepthCache
			if lc.Indepth && cachePath == "" {
				cachePath = defaultLanguagesIndepthCachePath
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

	// Music precedes WakaTime to match upstream's sidebar layout.
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
