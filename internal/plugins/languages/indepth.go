package languages

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	enry "github.com/go-enry/go-enry/v2"
	"github.com/google/go-github/v66/github"
	"github.com/twangodev/gmetrics/internal/plugin"
	"golang.org/x/sync/errgroup"
)

// maxFileSize is the upper bound on file size, in bytes, that indepth will
// actually read and classify. Larger blobs are skipped: they're almost
// always vendored datasets, generated artefacts, or binary blobs that
// enry.IsBinary would flag anyway, and reading them through go-git's
// in-memory storage is expensive.
const maxFileSize = 5 * 1024 * 1024 // 5 MiB

// indepthConcurrency is the maximum number of repositories cloned in
// parallel. Each clone is bandwidth-bound (the git protocol fetch) and
// memory-bound (we keep the worktree in memory.NewStorage()); 4 strikes a
// reasonable balance and matches the upstream JS implementation's default.
const indepthConcurrency = 4

// fetchIndepth lists the user's non-fork repositories via REST, shallow-
// clones each into an in-memory storage, walks the HEAD tree, and uses
// go-enry to classify each file. Bytes per language are aggregated and
// passed through the same assemble() pipeline as the non-indepth path so
// the resulting Data shape is identical.
//
// The function is best-effort: a per-repo failure is logged and skipped
// rather than aborting the whole fetch. The function honours the supplied
// ctx — once ctx is done, in-flight clones are cancelled and the aggregate
// returns whatever it has accumulated so far.
func fetchIndepth(ctx context.Context, env *plugin.Env, cfg Config) (Data, error) {
	if env.REST == nil {
		return Data{}, fmt.Errorf("languages: fetch indepth: env.REST is nil")
	}
	login := env.Login
	if login == "" {
		login = env.User.Login
	}
	if login == "" {
		return Data{}, fmt.Errorf("languages: fetch indepth: env.Login is empty")
	}

	repos, err := listUserRepos(ctx, env, login, cfg)
	if err != nil {
		return Data{}, fmt.Errorf("languages: fetch indepth: list repos: %w", err)
	}

	// Shared accumulator. Guarded by a mutex since errgroup workers
	// concurrently mutate it.
	var mu sync.Mutex
	bytes := map[string]int{}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(indepthConcurrency)

	for _, repo := range repos {
		repo := repo // capture
		g.Go(func() error {
			cloneURL := repo.GetCloneURL()
			if cloneURL == "" {
				return nil
			}
			repoBytes, err := walkRepo(gctx, cloneURL)
			if err != nil {
				// Best-effort: log and continue.
				if env.Log != nil {
					env.Log.Warn("languages: indepth repo skipped",
						"repo", repo.GetFullName(), "err", err)
				}
				return nil
			}
			mu.Lock()
			for name, n := range repoBytes {
				bytes[name] += n
			}
			mu.Unlock()
			return nil
		})
	}

	// We never return an error from the worker, so g.Wait() can only fail
	// if the context is cancelled. Either way we proceed to assemble what
	// we've collected so far — partial results are better than nothing.
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return Data{}, err
	}

	// In indepth mode the GraphQL color hint isn't available; assemble()
	// will fall back to defaultColors / enry.GetColor.
	return assemble(cfg, bytes, nil, true), nil
}

// listUserRepos pages through the REST repositories listing and returns
// non-fork repos, capped at cfg.Limit*5 to bound work. We over-fetch
// relative to Limit because some of those repos will be tiny / empty /
// fail to clone — having a few extra makes the bar more representative.
func listUserRepos(ctx context.Context, env *plugin.Env, login string, cfg Config) ([]*github.Repository, error) {
	cap := cfg.Limit * 5
	if cap < cfg.Limit {
		cap = cfg.Limit
	}
	opts := &github.RepositoryListOptions{
		Type: "owner",
		Sort: "updated",
		ListOptions: github.ListOptions{
			PerPage: 50,
		},
	}
	var all []*github.Repository
	for {
		repos, resp, err := env.REST.Repositories.List(ctx, login, opts)
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			if r.GetFork() {
				continue
			}
			all = append(all, r)
			if len(all) >= cap {
				return all, nil
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

// walkRepo clones a single repo into memory and aggregates language bytes
// by walking the HEAD tree. Skips vendor / documentation / generated /
// binary files via go-enry's path heuristics.
func walkRepo(ctx context.Context, cloneURL string) (map[string]int, error) {
	storer := memory.NewStorage()
	repo, err := git.CloneContext(ctx, storer, nil, &git.CloneOptions{
		URL:          cloneURL,
		Depth:        1,
		SingleBranch: true,
		NoCheckout:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("clone: %w", err)
	}
	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("head: %w", err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("tree: %w", err)
	}

	out := map[string]int{}
	err = tree.Files().ForEach(func(f *object.File) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := f.Name
		if enry.IsVendor(path) || enry.IsDocumentation(path) {
			return nil
		}
		size := f.Size
		if size <= 0 || size > maxFileSize {
			return nil
		}
		lang, _ := enry.GetLanguageByFilename(path)
		// Only read content if we still need to decide (binary check,
		// or no filename verdict and content classification may help).
		var content []byte
		if lang == "" {
			// Read content and let enry classify; for files we can't
			// open we just skip.
			s, err := f.Contents()
			if err != nil {
				return nil
			}
			content = []byte(s)
			if enry.IsBinary(content) {
				return nil
			}
			lang = enry.GetLanguage(path, content)
		}
		if lang == "" {
			return nil
		}
		// Generated check requires content for some matchers; opening
		// every file is expensive but skipping generated code is
		// important for accuracy. Read lazily only when we don't already
		// have content in hand.
		if content == nil {
			// Cheaper IsGenerated test: path-only matchers cover the
			// common cases (e.g. minified bundles, *_pb.go, etc.).
			if enry.IsGenerated(path, nil) {
				return nil
			}
		} else if enry.IsGenerated(path, content) {
			return nil
		}
		out[lang] += int(size)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	return out, nil
}
