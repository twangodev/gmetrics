package languages

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	enry "github.com/go-enry/go-enry/v2"
	gitcmd "github.com/gogs/git-module"
	"github.com/google/go-github/v66/github"
	"github.com/shurcooL/githubv4"
	"github.com/twangodev/gmetrics/internal/plugin"
	"golang.org/x/sync/errgroup"
)

const (
	// Eight workers saturated network and CPU in the representative full-history
	// benchmark without multiplying GitHub API pressure excessively.
	indepthConcurrency = 8
	// Repository walks obey caller cancellation but intentionally have no fixed
	// total deadline; large first runs may legitimately take several minutes.
	noTimeout = time.Duration(-1)
	// Small contributed histories are cheaper through REST than a network clone.
	apiThreshold = 50

	// `git backfill` first shipped in Git 2.49.
	backfillMinGitMajor = 2
	backfillMinGitMinor = 49
	// A larger batch avoids excessive backfill negotiation on long histories.
	backfillMinBatchSize = 50_000
	// Selective mode passes paths through argv, so bound both argv growth and the
	// proportion of history selected. These cutoffs won in the full-user benchmark.
	selectiveBackfillMaxPaths   = 4_096
	selectiveBackfillMaxPercent = 25

	// Abort only when Git transfer speed stays below 1 KiB/s for two minutes.
	// This prevents dead connections without reinstating a total repository timeout.
	gitLowSpeedBytesPerSecond = 1_024
	gitLowSpeedWindowSeconds  = 120
)

var gitEnv = []string{
	"GIT_TERMINAL_PROMPT=0",
	"GIT_CONFIG_COUNT=2",
	"GIT_CONFIG_KEY_0=http.lowSpeedLimit",
	"GIT_CONFIG_VALUE_0=" + strconv.Itoa(gitLowSpeedBytesPerSecond),
	"GIT_CONFIG_KEY_1=http.lowSpeedTime",
	"GIT_CONFIG_VALUE_1=" + strconv.Itoa(gitLowSpeedWindowSeconds),
}

type walkErrKind int

const (
	walkErrOther walkErrKind = iota
	walkErrNoAccess
	walkErrRateLimit
	walkErrTimeout
)

func gitEnvWithToken(token string) []string {
	if token == "" {
		return gitEnv
	}
	env := append([]string(nil), gitEnv...)
	for i, value := range env {
		if strings.HasPrefix(value, "GIT_CONFIG_COUNT=") {
			env[i] = "GIT_CONFIG_COUNT=3"
			break
		}
	}
	credentials := base64.StdEncoding.EncodeToString([]byte("oauth2:" + token))
	return append(env,
		"GIT_CONFIG_KEY_2=http.extraHeader",
		"GIT_CONFIG_VALUE_2=Authorization: Basic "+credentials,
	)
}

func classifyWalkErr(err error) (walkErrKind, string) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return walkErrTimeout, "timed out"
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "terminal prompts disabled"),
		strings.Contains(s, "Repository not found"),
		strings.Contains(s, "Authentication failed"),
		strings.Contains(s, "could not read Username"):
		return walkErrNoAccess, "no access"
	case strings.Contains(s, "rate limit exceeded"),
		strings.Contains(s, "API rate limit"),
		strings.Contains(s, "secondary rate limit"):
		return walkErrRateLimit, "rate limited"
	default:
		return walkErrOther, "clone or walk failed"
	}
}

const loginPlaceholder = ".user.login"

func buildAuthorPredicates(ctx context.Context, env *plugin.Env, cfg Config) []string {
	login := env.Login
	if login == "" {
		login = env.User.Login
	}
	loginLower := strings.ToLower(login)
	dbID := env.User.DatabaseID

	seen := map[string]struct{}{}
	out := make([]string, 0, len(cfg.CommitsAuthoring)+4)
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return
		}
		if _, dup := seen[s]; dup {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	for _, raw := range cfg.CommitsAuthoring {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if entry == loginPlaceholder {
			if loginLower == "" {
				continue
			}
			add(loginLower)
			add(loginLower + "@users.noreply.github.com")
			if dbID > 0 {
				add(fmt.Sprintf("%d+%s@users.noreply.github.com", dbID, loginLower))
			}
			continue
		}
		add(entry)
	}

	if env.REST != nil && login != "" {
		keys, _, err := env.REST.Users.ListGPGKeys(ctx, login, nil)
		if err == nil {
			for _, k := range keys {
				for _, e := range k.Emails {
					if email := e.GetEmail(); email != "" {
						add(email)
					}
				}
			}
		} else if env.Log != nil {
			env.Log.Debug("languages: gpg keys fetch failed", "user", login, "err", err)
		}
	}

	return out
}

// Production clone walks delegate to `git log --author=`; this stays for the API path and tests.
func authorMatches(preds []string, name, email string) bool {
	if len(preds) == 0 {
		return false
	}
	header := strings.ToLower(name + " <" + email + ">")
	for _, p := range preds {
		if strings.Contains(header, p) {
			return true
		}
	}
	return false
}

type indepthState struct {
	cachePath string
	cache     *cacheFile

	cacheMu      sync.Mutex
	checkpointMu sync.Mutex
	aggregateMu  sync.Mutex
	bytes        map[string]int

	done            int32
	totalAuthored   int64
	totalFiles      int64
	totalLines      int64
	skippedNoAccess int32
	failedRateLimit int32
}

func newIndepthState(cachePath string, cache *cacheFile) *indepthState {
	return &indepthState{
		cachePath: cachePath,
		cache:     cache,
		bytes:     map[string]int{},
	}
}

func (s *indepthState) cachedRepo(name string) (repoEntry, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache.Repos[name]
	return entry, ok
}

func (s *indepthState) recordRepo(name string, entry repoEntry, checkpoint bool) error {
	atomic.AddInt64(&s.totalAuthored, int64(entry.Commits))
	atomic.AddInt64(&s.totalFiles, int64(entry.Files))
	atomic.AddInt64(&s.totalLines, int64(entry.Lines))
	s.aggregateMu.Lock()
	for language, count := range entry.Bytes {
		s.bytes[language] += count
	}
	s.aggregateMu.Unlock()

	s.cacheMu.Lock()
	s.cache.Repos[name] = entry
	s.cacheMu.Unlock()
	if checkpoint {
		return s.saveCacheSnapshot()
	}
	return nil
}

func (s *indepthState) saveCacheSnapshot() error {
	// Serialize renames so an older snapshot can never overwrite a newer one,
	// while allowing workers to update the in-memory cache during disk I/O.
	s.checkpointMu.Lock()
	defer s.checkpointMu.Unlock()
	s.cacheMu.Lock()
	snapshot := s.cache.clone()
	s.cacheMu.Unlock()
	return saveCache(s.cachePath, snapshot)
}

func (s *indepthState) pruneAndSave(tasks []repoTask) error {
	seen := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		seen[task.FullName] = struct{}{}
	}
	s.cacheMu.Lock()
	s.cache.prune(seen)
	s.cacheMu.Unlock()
	return s.saveCacheSnapshot()
}

func (s *indepthState) result(cfg Config) Data {
	s.aggregateMu.Lock()
	counts := cloneLanguageBytes(s.bytes)
	s.aggregateMu.Unlock()
	data := assemble(cfg, counts, nil, true)
	data.IndepthCommits = int(atomic.LoadInt64(&s.totalAuthored))
	data.IndepthFiles = int(atomic.LoadInt64(&s.totalFiles))
	data.IndepthLines = int(atomic.LoadInt64(&s.totalLines))
	return data
}

type indepthRunner struct {
	env       *plugin.Env
	preds     []string
	login     string
	total     int
	quiet     bool
	state     *indepthState
	startedAt time.Time
}

func (r *indepthRunner) processRepo(ctx context.Context, task repoTask) error {
	t0 := time.Now()
	owner, name := splitFullName(task.FullName)
	previous, cacheHit := r.state.cachedRepo(task.FullName)

	if r.env.Log != nil && !r.quiet {
		r.env.Log.Info("languages: walking",
			"repo", task.FullName, "source", task.Source, "size_kb", task.SizeKB)
	}

	entry, source, err := resolveRepo(task, previous, cacheHit,
		func() (walkResult, string, error) { return walkTask(ctx, r.env, task, r.preds, r.login) },
		func(base repoEntry) (repoEntry, foldOutcome, error) {
			return walkRepoViaCompare(ctx, r.env, owner, name, base.HeadSHA, task.DefaultBranch, r.preds, base)
		},
	)
	n := atomic.AddInt32(&r.state.done, 1)
	if err != nil {
		kind, reason := classifyWalkErr(err)
		if kind == walkErrNoAccess {
			atomic.AddInt32(&r.state.skippedNoAccess, 1)
			return nil
		}
		if kind == walkErrRateLimit {
			atomic.AddInt32(&r.state.failedRateLimit, 1)
		}
		if r.env.Log != nil {
			repo := task.FullName
			if r.quiet {
				repo = "***"
			}
			r.env.Log.Warn("languages: walk failed",
				"repo", repo, "i", n, "total", r.total,
				"dur_ms", time.Since(t0).Milliseconds(), "reason", reason)
		}
		return nil
	}

	if source != "cache" && source != "fold" && entry.HeadSHA == "" {
		if head := resolveHeadSHA(ctx, r.env, owner, name, task.DefaultBranch); head != "" {
			entry.HeadSHA = head
		}
	}
	if err := r.state.recordRepo(task.FullName, entry, source != "cache"); err != nil && r.env.Log != nil {
		r.env.Log.Warn("languages: cache checkpoint failed", "err", err)
	}

	if r.env.Log != nil && !r.quiet {
		r.env.Log.Info("languages: walked",
			"repo", task.FullName, "i", n, "total", r.total,
			"source", source, "dur_ms", time.Since(t0).Milliseconds(),
			"authored", entry.Commits, "langs", len(entry.Bytes))
	}
	return nil
}

func (r *indepthRunner) logSummary() {
	if r.env.Log == nil {
		return
	}
	r.state.aggregateMu.Lock()
	languages := len(r.state.bytes)
	r.state.aggregateMu.Unlock()
	r.env.Log.Info("languages: indepth complete",
		"total_repos", r.total,
		"elapsed_s", int(time.Since(r.startedAt).Seconds()),
		"authored_commits", atomic.LoadInt64(&r.state.totalAuthored),
		"files", atomic.LoadInt64(&r.state.totalFiles),
		"lines", atomic.LoadInt64(&r.state.totalLines),
		"langs", languages,
		"skipped_no_access", atomic.LoadInt32(&r.state.skippedNoAccess),
		"failed_rate_limit", atomic.LoadInt32(&r.state.failedRateLimit))
}

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

	preds := buildAuthorPredicates(ctx, env, cfg)
	if env.Log != nil {
		env.Log.Info("languages: indepth author predicates", "count", len(preds))
	}
	if len(preds) == 0 {
		return Data{}, fmt.Errorf("languages: fetch indepth: no commits_authoring patterns configured")
	}

	if _, err := gitcmd.BinVersion(); err != nil {
		return Data{}, fmt.Errorf("languages: fetch indepth: git binary not available: %w", err)
	}

	tasks, err := buildRepoTasks(ctx, env, login, cfg)
	if err != nil {
		return Data{}, fmt.Errorf("languages: fetch indepth: list repos: %w", err)
	}
	if env.Log != nil {
		env.Log.Info("languages: indepth walking repos",
			"count", len(tasks), "concurrency", indepthConcurrency)
	}

	ph := predHash(preds)
	cache := loadCache(cfg.IndepthCachePath)
	if cache.PredHash != ph {
		cache = newCache(ph)
	}
	state := newIndepthState(cfg.IndepthCachePath, cache)
	runner := &indepthRunner{
		env:       env,
		preds:     preds,
		login:     login,
		total:     len(tasks),
		quiet:     os.Getenv("CI") == "true",
		state:     state,
		startedAt: time.Now(),
	}

	var stopHeartbeat chan struct{}
	if runner.quiet && env.Log != nil {
		stopHeartbeat = make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil && env.Log != nil {
					env.Log.Error("languages: heartbeat panicked", "panic", r, "stack", string(debug.Stack()))
				}
			}()
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-stopHeartbeat:
					return
				case <-t.C:
					n := atomic.LoadInt32(&state.done)
					env.Log.Info("languages: indepth progress",
						"completed", n, "total", len(tasks),
						"elapsed_s", int(time.Since(runner.startedAt).Seconds()))
				}
			}
		}()
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(indepthConcurrency)

	for _, t := range tasks {
		t := t
		g.Go(func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					repo := t.FullName
					if runner.quiet {
						repo = "***"
					}
					if env.Log != nil {
						env.Log.Error("languages: walk panicked", "repo", repo, "panic", r, "stack", string(debug.Stack()))
					}
					err = nil
				}
			}()
			return runner.processRepo(gctx, t)
		})
	}

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		if stopHeartbeat != nil {
			close(stopHeartbeat)
		}
		return Data{}, err
	}
	if stopHeartbeat != nil {
		close(stopHeartbeat)
	}
	if err := ctx.Err(); err != nil {
		return Data{}, err
	}

	if err := state.pruneAndSave(tasks); err != nil && env.Log != nil {
		env.Log.Warn("languages: cache save failed", "err", err)
	}

	runner.logSummary()
	return state.result(cfg), nil
}

func listUserRepos(ctx context.Context, env *plugin.Env, login string, cfg Config) ([]*github.Repository, error) {
	limit := repoSelectionLimit(cfg)
	opts := &github.RepositoryListOptions{
		Sort:        "updated",
		ListOptions: github.ListOptions{PerPage: min(cfg.RepoBatch, 100)},
	}
	listLogin := login
	viewer, _, viewerErr := env.REST.Users.Get(ctx, "")
	if viewerErr != nil {
		return nil, fmt.Errorf("resolve authenticated GitHub user: %w", viewerErr)
	}
	if strings.EqualFold(viewer.GetLogin(), login) {
		// The authenticated endpoint is required to include private repositories
		// and supports the exact affiliation filter.
		listLogin = ""
		opts.Visibility = "all"
		opts.Affiliation = strings.Join(normalizedRepoAffiliations(cfg.RepoAffiliations), ",")
	} else {
		// GitHub's public user endpoint only offers the coarser owner/member types.
		// Production uses the exact GraphQL path; this is a REST-only fallback.
		opts.Type = publicRepoType(cfg.RepoAffiliations)
	}
	var all []*github.Repository
	for hops := 0; hops < maxPaginationHops; hops++ {
		repos, resp, err := env.REST.Repositories.List(ctx, listLogin, opts)
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			if r.GetFork() {
				continue
			}
			all = append(all, r)
			if len(all) >= limit {
				return all, nil
			}
		}
		if resp == nil || resp.NextPage == 0 {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
	return nil, fmt.Errorf("REST repository pagination exceeded %d pages", maxPaginationHops)
}

func publicRepoType(affiliations []string) string {
	affiliations = normalizedRepoAffiliations(affiliations)
	hasOwner := false
	hasMember := false
	for _, affiliation := range affiliations {
		if affiliation == "owner" {
			hasOwner = true
		} else {
			hasMember = true
		}
	}
	switch {
	case hasOwner && !hasMember:
		return "owner"
	case !hasOwner && hasMember:
		return "member"
	default:
		return "all"
	}
}

func normalizedRepoAffiliations(affiliations []string) []string {
	if len(affiliations) == 0 {
		return []string{"owner"}
	}
	return affiliations
}

func repoSelectionLimit(cfg Config) int {
	if cfg.RepoMax > 0 {
		return cfg.RepoMax
	}
	return defaultConfig().RepoMax
}

type affiliatedReposQuery struct {
	User struct {
		Repositories struct {
			Nodes []struct {
				NameWithOwner    githubv4.String
				URL              githubv4.URI
				DiskUsage        githubv4.Int
				PushedAt         githubv4.DateTime
				DefaultBranchRef struct {
					Name githubv4.String
				}
			}
			PageInfo struct {
				EndCursor   githubv4.String
				HasNextPage githubv4.Boolean
			}
		} `graphql:"repositories(first: $first, after: $after, ownerAffiliations: $affiliations, isFork: false, orderBy: {field: UPDATED_AT, direction: DESC})"`
	} `graphql:"user(login: $login)"`
}

func listAffiliatedRepoTasks(ctx context.Context, env *plugin.Env, login string, cfg Config) ([]repoTask, error) {
	limit := repoSelectionLimit(cfg)
	if env.GraphQL == nil {
		repos, err := listUserRepos(ctx, env, login, cfg)
		if err != nil {
			return nil, err
		}
		tasks := make([]repoTask, 0, len(repos))
		for _, repo := range repos {
			tasks = append(tasks, repoTask{
				FullName:      repo.GetFullName(),
				CloneURL:      repo.GetCloneURL(),
				SizeKB:        repo.GetSize(),
				Source:        "affiliated",
				PushedAt:      repo.GetPushedAt().UTC().Format(time.RFC3339),
				DefaultBranch: repo.GetDefaultBranch(),
			})
		}
		return tasks, nil
	}

	batch := min(cfg.RepoBatch, 100, limit)
	vars := map[string]any{
		"login":        githubv4.String(login),
		"first":        githubv4.Int(batch),
		"after":        (*githubv4.String)(nil),
		"affiliations": repoAffiliationsToEnum(cfg.RepoAffiliations),
	}
	tasks := make([]repoTask, 0, limit)
	for hops := 0; hops < maxPaginationHops; hops++ {
		var q affiliatedReposQuery
		if err := env.GraphQL.Query(ctx, &q, vars); err != nil {
			return nil, err
		}
		for _, repo := range q.User.Repositories.Nodes {
			cloneURL := ""
			if repo.URL.URL != nil {
				cloneURL = repo.URL.String() + ".git"
			}
			if repo.NameWithOwner == "" || cloneURL == "" {
				continue
			}
			tasks = append(tasks, repoTask{
				FullName:      string(repo.NameWithOwner),
				CloneURL:      cloneURL,
				SizeKB:        int(repo.DiskUsage),
				Source:        "affiliated",
				PushedAt:      repo.PushedAt.UTC().Format(time.RFC3339),
				DefaultBranch: string(repo.DefaultBranchRef.Name),
			})
			if len(tasks) >= limit {
				return tasks, nil
			}
		}
		if !q.User.Repositories.PageInfo.HasNextPage {
			return tasks, nil
		}
		cursor := q.User.Repositories.PageInfo.EndCursor
		vars["after"] = &cursor
		vars["first"] = githubv4.Int(min(batch, limit-len(tasks)))
	}
	return nil, fmt.Errorf("repository pagination exceeded %d pages", maxPaginationHops)
}

type walkResult struct {
	Bytes    map[string]int
	Commits  int
	Files    int
	Lines    int
	HeadSHA  string
	CloneDur time.Duration
	LogDur   time.Duration
}

const commitMarker = "__GMETRICS_COMMIT__"

func walkRepo(ctx context.Context, cloneURL string, preds []string) (walkResult, error) {
	return walkRepoWithEnv(ctx, cloneURL, preds, gitEnv)
}

func walkRepoAuthenticated(ctx context.Context, cloneURL string, preds []string, token string) (walkResult, error) {
	return walkRepoWithEnv(ctx, cloneURL, preds, gitEnvWithToken(token))
}

func walkRepoWithEnv(ctx context.Context, cloneURL string, preds, commandEnv []string) (walkResult, error) {
	version, err := gitcmd.BinVersion()
	if err == nil && gitSupportsBackfill(version) {
		res, backfillErr := walkRepoBackfillWithEnv(ctx, cloneURL, preds, commandEnv)
		if backfillErr == nil {
			return res, nil
		}
		res, fullErr := walkRepoFullWithEnv(ctx, cloneURL, preds, commandEnv)
		if fullErr == nil {
			return res, nil
		}
		return res, fmt.Errorf("backfill walk failed: %v; full fallback failed: %w", backfillErr, fullErr)
	}
	return walkRepoFullWithEnv(ctx, cloneURL, preds, commandEnv)
}

func gitSupportsBackfill(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return false
	}
	return major > backfillMinGitMajor || major == backfillMinGitMajor && minor >= backfillMinGitMinor
}

func walkRepoFull(ctx context.Context, cloneURL string, preds []string) (walkResult, error) {
	return walkRepoFullWithEnv(ctx, cloneURL, preds, gitEnv)
}

func walkRepoFullWithEnv(ctx context.Context, cloneURL string, preds, commandEnv []string) (walkResult, error) {
	dir, err := os.MkdirTemp("", "gmetrics-clone-*")
	if err != nil {
		return walkResult{}, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(dir)

	tClone := time.Now()
	if err := gitcmd.Clone(cloneURL, dir, gitcmd.CloneOptions{
		Bare:  true,
		Quiet: true,
		CommandOptions: gitcmd.CommandOptions{
			Context: ctx,
			Envs:    commandEnv,
			Timeout: noTimeout,
		},
	}); err != nil {
		return walkResult{}, fmt.Errorf("clone: %w", err)
	}
	cloneDur := time.Since(tClone)
	headSHA, empty, err := resolveRepoHEAD(ctx, dir)
	if err != nil {
		return walkResult{}, fmt.Errorf("resolve cloned repository HEAD: %w", err)
	}
	if empty {
		return walkResult{Bytes: map[string]int{}, CloneDur: cloneDur}, nil
	}
	return scanRepoNumstat(ctx, dir, preds, nil, headSHA, cloneDur, commandEnv)
}

func resolveRepoHEAD(ctx context.Context, dir string) (sha string, empty bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	out, headErr := gitcmd.NewCommand("rev-parse", "--verify", "HEAD").
		AddOptions(gitcmd.CommandOptions{Context: ctx, Envs: gitEnv, Timeout: noTimeout}).
		RunInDir(dir)
	if headErr == nil {
		sha = strings.TrimSpace(string(out))
		if sha == "" {
			return "", false, fmt.Errorf("git rev-parse returned an empty HEAD")
		}
		return sha, false, nil
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}

	// An unborn repository has no HEAD and no reachable commits. Any other
	// rev-parse failure is corruption or an invalid clone and must not cache zeroes.
	refs, refsErr := gitcmd.NewCommand("rev-list", "--all", "--max-count=1").
		AddOptions(gitcmd.CommandOptions{Context: ctx, Envs: gitEnv, Timeout: noTimeout}).
		RunInDir(dir)
	if refsErr != nil {
		return "", false, fmt.Errorf("resolve HEAD: %v; inspect refs: %w", headErr, refsErr)
	}
	if strings.TrimSpace(string(refs)) == "" {
		return "", true, nil
	}
	return "", false, fmt.Errorf("resolve HEAD while repository contains commits: %w", headErr)
}

func scanRepoNumstat(ctx context.Context, dir string, preds, pathspecs []string, headSHA string, cloneDur time.Duration, commandEnv []string) (walkResult, error) {
	args := []string{
		"log",
		"--no-merges",
		"--numstat",
		"--regexp-ignore-case",
		// tformat: prefix forces git to treat the rest as literal, not a named built-in format.
		"--format=tformat:" + commitMarker,
	}
	for _, p := range preds {
		args = append(args, "--author="+regexp.QuoteMeta(p))
	}
	args = append(args, "HEAD")
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}

	pr, pw := io.Pipe()
	var stderrBuf bytes.Buffer
	runErrCh := make(chan error, 1)
	tLog := time.Now()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				runErrCh <- fmt.Errorf("panic: %v\n%s", r, debug.Stack())
				_ = pw.Close()
			}
		}()
		// AddOptions overwrites Command.ctx, so ctx must travel inside CommandOptions, not NewCommandWithContext.
		runErrCh <- gitcmd.NewCommand(args...).
			AddOptions(gitcmd.CommandOptions{
				Context: ctx,
				Envs:    commandEnv,
				Timeout: noTimeout,
			}).
			RunInDirPipeline(pw, &stderrBuf, dir)
		_ = pw.Close()
	}()

	res := walkResult{Bytes: map[string]int{}, CloneDur: cloneDur, HeadSHA: headSHA}
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if line == commitMarker {
			res.Commits++
			continue
		}
		lang, added, ok := classifyNumstatLine(line)
		if !ok {
			continue
		}
		res.Bytes[lang] += added
		res.Files++
		res.Lines += added
	}
	scanErr := scanner.Err()
	runErr := <-runErrCh
	res.LogDur = time.Since(tLog)
	if runErr != nil {
		return res, fmt.Errorf("git log: %w: %s", runErr, strings.TrimSpace(stderrBuf.String()))
	}
	if scanErr != nil {
		return res, fmt.Errorf("scan: %w", scanErr)
	}
	return res, nil
}

type backfillProbe struct {
	Commits         int
	FileChanges     int
	EligibleChanges int
	EligiblePaths   []string
}

func walkRepoBackfill(ctx context.Context, cloneURL string, preds []string) (walkResult, error) {
	return walkRepoBackfillWithEnv(ctx, cloneURL, preds, gitEnv)
}

func walkRepoBackfillWithEnv(ctx context.Context, cloneURL string, preds, commandEnv []string) (walkResult, error) {
	dir, err := os.MkdirTemp("", "gmetrics-backfill-*")
	if err != nil {
		return walkResult{}, fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(dir)

	tClone := time.Now()
	if err := gitcmd.Clone(cloneURL, dir, gitcmd.CloneOptions{
		Quiet: true,
		CommandOptions: gitcmd.CommandOptions{
			Args:    []string{"--filter=blob:none", "--no-checkout", "--single-branch", "--no-tags"},
			Context: ctx,
			Envs:    commandEnv,
			Timeout: noTimeout,
		},
	}); err != nil {
		return walkResult{}, fmt.Errorf("blobless clone: %w", err)
	}
	cloneDur := time.Since(tClone)
	headSHA, empty, err := resolveRepoHEAD(ctx, dir)
	if err != nil {
		return walkResult{}, fmt.Errorf("resolve cloned repository HEAD: %w", err)
	}
	if empty {
		return walkResult{Bytes: map[string]int{}, CloneDur: cloneDur}, nil
	}

	probe, err := probeBackfill(ctx, dir, preds)
	if err != nil {
		return walkResult{}, err
	}
	if probe.EligibleChanges == 0 {
		return walkResult{Bytes: map[string]int{}, Commits: probe.Commits, HeadSHA: headSHA, CloneDur: cloneDur}, nil
	}

	var pathspecs []string
	if useSelectiveBackfill(probe) {
		pathspecs = make([]string, len(probe.EligiblePaths))
		for i, path := range probe.EligiblePaths {
			pathspecs[i] = ":(top,literal)" + path
		}
	}
	if err := runBackfill(ctx, dir, preds, pathspecs, commandEnv); err != nil {
		return walkResult{}, err
	}
	res, err := scanRepoNumstat(ctx, dir, preds, pathspecs, headSHA, cloneDur, commandEnv)
	res.Commits = probe.Commits
	return res, err
}

func useSelectiveBackfill(probe backfillProbe) bool {
	if probe.EligibleChanges == 0 {
		return true
	}
	return probe.FileChanges > 0 &&
		len(probe.EligiblePaths) <= selectiveBackfillMaxPaths &&
		int64(probe.EligibleChanges)*100 <= int64(probe.FileChanges)*selectiveBackfillMaxPercent
}

func runBackfill(ctx context.Context, dir string, preds, pathspecs, commandEnv []string) error {
	args := []string{"backfill", fmt.Sprintf("--min-batch-size=%d", backfillMinBatchSize), "--no-merges"}
	for _, p := range preds {
		args = append(args, "--author="+regexp.QuoteMeta(p))
	}
	args = append(args, "HEAD")
	if len(pathspecs) > 0 {
		args = append(args, "--")
		args = append(args, pathspecs...)
	}
	var stderr bytes.Buffer
	err := gitcmd.NewCommand(args...).
		AddOptions(gitcmd.CommandOptions{Context: ctx, Envs: commandEnv, Timeout: noTimeout}).
		RunInDirPipeline(io.Discard, &stderr, dir)
	if err != nil {
		return fmt.Errorf("git backfill: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func probeBackfill(ctx context.Context, dir string, preds []string) (backfillProbe, error) {
	args := []string{"log", "--no-merges", "--raw", "--find-renames=100%", "-z", "--format=tformat:" + commitMarker}
	for _, p := range preds {
		args = append(args, "--author="+regexp.QuoteMeta(p))
	}
	args = append(args, "HEAD")

	pr, pw := io.Pipe()
	var stderr bytes.Buffer
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- gitcmd.NewCommand(args...).
			AddOptions(gitcmd.CommandOptions{Context: ctx, Envs: gitEnv, Timeout: noTimeout}).
			RunInDirPipeline(pw, &stderr, dir)
		_ = pw.Close()
	}()

	probe, parseErr := parseRawProbe(bufio.NewReader(pr))
	if parseErr != nil {
		_ = pr.CloseWithError(parseErr)
	}
	runErr := <-runErrCh
	if runErr != nil {
		return probe, fmt.Errorf("git raw log: %w: %s", runErr, strings.TrimSpace(stderr.String()))
	}
	if parseErr != nil {
		return probe, parseErr
	}
	return probe, nil
}

func parseRawProbe(r *bufio.Reader) (backfillProbe, error) {
	probe := backfillProbe{}
	paths := map[string]struct{}{}
	for {
		token, err := readNULToken(r)
		if err == io.EOF && token == "" {
			break
		}
		if err != nil && err != io.EOF {
			return probe, fmt.Errorf("read raw log: %w", err)
		}
		token = strings.TrimLeft(token, "\n")
		switch {
		case token == commitMarker:
			probe.Commits++
		case strings.HasPrefix(token, ":"):
			fields := strings.Fields(token)
			if len(fields) == 0 {
				return probe, fmt.Errorf("parse raw log metadata %q", token)
			}
			status := fields[len(fields)-1]
			oldPath, pathErr := readNULToken(r)
			if pathErr != nil {
				return probe, fmt.Errorf("read raw log path: %w", pathErr)
			}
			newPath := oldPath
			if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
				newPath, pathErr = readNULToken(r)
				if pathErr != nil {
					return probe, fmt.Errorf("read raw log destination: %w", pathErr)
				}
			}
			probe.FileChanges++
			if !strings.HasPrefix(status, "D") && eligibleCodePath(newPath) {
				probe.EligibleChanges++
				paths[newPath] = struct{}{}
				if newPath != oldPath {
					paths[oldPath] = struct{}{}
				}
			}
		}
		if err == io.EOF {
			break
		}
	}
	probe.EligiblePaths = make([]string, 0, len(paths))
	for path := range paths {
		probe.EligiblePaths = append(probe.EligiblePaths, path)
	}
	sort.Strings(probe.EligiblePaths)
	return probe, nil
}

func readNULToken(r *bufio.Reader) (string, error) {
	token, err := r.ReadString(0)
	if len(token) > 0 && token[len(token)-1] == 0 {
		token = token[:len(token)-1]
	}
	return token, err
}

func eligibleCodePath(path string) bool {
	if path == "" || excludedPath(path) || enry.IsVendor(path) || enry.IsDocumentation(path) || enry.IsGenerated(path, nil) {
		return false
	}
	lang := classifyPath(path)
	if lang == "" {
		return false
	}
	switch enry.GetLanguageType(lang) {
	case enry.Programming, enry.Markup:
		return true
	default:
		return false
	}
}

const numstatBinaryMarker = "-"

func classifyNumstatLine(line string) (string, int, bool) {
	const addedField, pathField, numstatFieldCount = 0, 2, 3
	parts := strings.SplitN(line, "\t", numstatFieldCount)
	if len(parts) != numstatFieldCount {
		return "", 0, false
	}
	added, path := parts[addedField], parts[pathField]
	if added == numstatBinaryMarker {
		return "", 0, false
	}
	addedN, err := strconv.Atoi(added)
	if err != nil || addedN <= 0 {
		return "", 0, false
	}
	// Renames render as "old => new" or "{a => b}/rest" — keep the new path.
	if i := strings.LastIndex(path, "=> "); i >= 0 {
		path = strings.TrimRight(path[i+3:], "}")
	}
	if excludedPath(path) || enry.IsVendor(path) || enry.IsDocumentation(path) || enry.IsGenerated(path, nil) {
		return "", 0, false
	}
	lang := classifyPath(path)
	if lang == "" {
		return "", 0, false
	}
	switch enry.GetLanguageType(lang) {
	case enry.Programming, enry.Markup:
		return lang, addedN, true
	}
	return "", 0, false
}

// GetLanguages disambiguates ambiguous extensions GetLanguageByFilename misses, e.g. .md -> Markdown not "GCC Machine Description".
func classifyPath(path string) string {
	if lang, _ := enry.GetLanguageByFilename(path); lang != "" {
		return lang
	}
	for _, cand := range enry.GetLanguages(path, nil) {
		switch enry.GetLanguageType(cand) {
		case enry.Programming, enry.Markup:
			return cand
		}
	}
	return ""
}

type repoTask struct {
	FullName      string
	CloneURL      string
	SizeKB        int
	Source        string
	PushedAt      string
	DefaultBranch string
}

func buildRepoTasks(ctx context.Context, env *plugin.Env, login string, cfg Config) ([]repoTask, error) {
	limit := repoSelectionLimit(cfg)
	affiliated, err := listAffiliatedRepoTasks(ctx, env, login, cfg)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	tasks := make([]repoTask, 0, limit)
	for _, task := range affiliated {
		if task.FullName == "" || task.CloneURL == "" {
			continue
		}
		if _, duplicate := seen[task.FullName]; duplicate {
			continue
		}
		seen[task.FullName] = struct{}{}
		tasks = append(tasks, task)
		if len(tasks) >= limit {
			return tasks, nil
		}
	}

	contributed, err := listContributedRepos(ctx, env, login, seen, limit-len(tasks))
	if err != nil {
		return nil, fmt.Errorf("list contributed repositories: %w", err)
	}
	for _, c := range contributed {
		if _, dup := seen[c.NameWithOwner]; dup {
			continue
		}
		if c.NameWithOwner == "" || c.CloneURL == "" {
			continue
		}
		seen[c.NameWithOwner] = struct{}{}
		tasks = append(tasks, repoTask{
			FullName:      c.NameWithOwner,
			CloneURL:      c.CloneURL,
			Source:        "contributed",
			PushedAt:      c.PushedAt,
			DefaultBranch: c.DefaultBranch,
		})
		if len(tasks) >= limit {
			break
		}
	}
	return tasks, nil
}

func walkTask(ctx context.Context, env *plugin.Env, t repoTask, preds []string, login string) (walkResult, string, error) {
	owner, name := splitFullName(t.FullName)

	if t.Source == "contributed" && env.REST != nil {
		if count, err := probeAuthorCommitCount(ctx, env, owner, name, login); err == nil && count < apiThreshold {
			res, err := walkRepoViaAPI(ctx, env, owner, name, preds)
			return res, "api", err
		}
	}

	res, err := walkRepoAuthenticated(ctx, t.CloneURL, preds, env.Token)
	if err == nil {
		return res, "clone", nil
	}

	if env.REST != nil && owner != "" && name != "" {
		fbRes, fbErr := walkRepoViaAPI(ctx, env, owner, name, preds)
		if fbErr == nil {
			return fbRes, "api-fallback", nil
		}
	}
	return res, "clone", err
}

type contribRepo struct {
	NameWithOwner string
	CloneURL      string
	PushedAt      string
	DefaultBranch string
}

func listContributedRepos(ctx context.Context, env *plugin.Env, login string, excluded map[string]struct{}, limit int) ([]contribRepo, error) {
	if env.GraphQL == nil || limit <= 0 {
		return nil, nil
	}
	var q struct {
		User struct {
			RepositoriesContributedTo struct {
				Nodes []struct {
					NameWithOwner    githubv4.String
					URL              githubv4.String
					PushedAt         githubv4.DateTime
					DefaultBranchRef struct {
						Name githubv4.String
					}
				}
				PageInfo struct {
					EndCursor   githubv4.String
					HasNextPage githubv4.Boolean
				}
			} `graphql:"repositoriesContributedTo(first: 100, after: $after, includeUserRepositories: false, contributionTypes: [COMMIT, PULL_REQUEST])"`
		} `graphql:"user(login: $login)"`
	}
	vars := map[string]any{
		"login": githubv4.String(login),
		"after": (*githubv4.String)(nil),
	}
	out := []contribRepo{}
	seen := make(map[string]struct{}, len(excluded))
	for name := range excluded {
		seen[name] = struct{}{}
	}
	for hops := 0; hops < maxPaginationHops; hops++ {
		if err := env.GraphQL.Query(ctx, &q, vars); err != nil {
			return nil, err
		}
		for _, n := range q.User.RepositoriesContributedTo.Nodes {
			name := string(n.NameWithOwner)
			url := string(n.URL)
			if name == "" || url == "" {
				continue
			}
			if _, duplicate := seen[name]; duplicate {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, contribRepo{
				NameWithOwner: name,
				CloneURL:      url + ".git",
				PushedAt:      n.PushedAt.UTC().Format(time.RFC3339),
				DefaultBranch: string(n.DefaultBranchRef.Name),
			})
			if len(out) >= limit {
				return out, nil
			}
		}
		if !q.User.RepositoriesContributedTo.PageInfo.HasNextPage {
			return out, nil
		}
		cursor := q.User.RepositoriesContributedTo.PageInfo.EndCursor
		vars["after"] = &cursor
	}
	return nil, fmt.Errorf("contributed repository pagination exceeded %d pages", maxPaginationHops)
}

func probeAuthorCommitCount(ctx context.Context, env *plugin.Env, owner, name, login string) (int, error) {
	commits, _, err := env.REST.Repositories.ListCommits(ctx, owner, name, &github.CommitsListOptions{
		Author:      login,
		ListOptions: github.ListOptions{PerPage: apiThreshold + 1},
	})
	if err != nil {
		return 0, err
	}
	return len(commits), nil
}

func walkRepoViaAPI(ctx context.Context, env *plugin.Env, owner, name string, preds []string) (walkResult, error) {
	res := walkResult{Bytes: map[string]int{}}
	seen := map[string]struct{}{}

	for _, predicate := range preds {
		opts := &github.CommitsListOptions{
			Author:      predicate,
			ListOptions: github.ListOptions{PerPage: 100},
		}
		complete := false
		for hops := 0; hops < maxPaginationHops; hops++ {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			commits, resp, err := env.REST.Repositories.ListCommits(ctx, owner, name, opts)
			if err != nil {
				return res, err
			}
			for _, commit := range commits {
				if len(commit.Parents) <= 1 {
					seen[commit.GetSHA()] = struct{}{}
				}
			}
			if resp == nil || resp.NextPage == 0 {
				complete = true
				break
			}
			opts.Page = resp.NextPage
		}
		if !complete {
			return res, fmt.Errorf("commit pagination for author %q exceeded %d pages", predicate, maxPaginationHops)
		}
	}

	res.Commits = len(seen)
	for sha := range seen {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		files, err := getCommitFiles(ctx, env, owner, name, sha)
		if err != nil {
			return res, fmt.Errorf("get commit %s: %w", sha, err)
		}
		accumulateCommit(&res, files)
	}
	return res, nil
}

func getCommitFiles(ctx context.Context, env *plugin.Env, owner, name, sha string) ([]*github.CommitFile, error) {
	opts := &github.ListOptions{PerPage: 100}
	var files []*github.CommitFile
	for hops := 0; hops < maxPaginationHops; hops++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		commit, resp, err := env.REST.Repositories.GetCommit(ctx, owner, name, sha, opts)
		if err != nil {
			return nil, err
		}
		files = append(files, commit.Files...)
		if resp == nil || resp.NextPage == 0 {
			return files, nil
		}
		opts.Page = resp.NextPage
	}
	return nil, fmt.Errorf("file pagination exceeded %d pages", maxPaginationHops)
}

func selectAuthoredSHAs(commits []*github.RepositoryCommit, preds []string) []string {
	var out []string
	for _, c := range commits {
		if len(c.Parents) > 1 {
			continue
		}
		a := c.GetCommit().GetAuthor()
		if authorMatches(preds, a.GetName(), a.GetEmail()) {
			out = append(out, c.GetSHA())
		}
	}
	return out
}

type foldOutcome int

const (
	foldApplied foldOutcome = iota
	foldRecompute
)

func walkRepoViaCompare(ctx context.Context, env *plugin.Env, owner, name, base, head string, preds []string, prev repoEntry) (repoEntry, foldOutcome, error) {
	opts := &github.ListOptions{PerPage: 100}
	cmp, _, err := env.REST.Repositories.CompareCommits(ctx, owner, name, base, head, opts)
	if err != nil {
		return prev, foldRecompute, err
	}
	switch cmp.GetStatus() {
	case "identical":
		return prev, foldApplied, nil
	case "behind", "diverged":
		return prev, foldRecompute, nil
	}
	if cmp.GetAheadBy() > apiThreshold {
		return prev, foldRecompute, nil
	}

	res := walkResult{Bytes: map[string]int{}}
	for _, sha := range selectAuthoredSHAs(cmp.Commits, preds) {
		if err := ctx.Err(); err != nil {
			return prev, foldRecompute, err
		}
		files, err := getCommitFiles(ctx, env, owner, name, sha)
		if err != nil {
			return prev, foldRecompute, fmt.Errorf("get commit %s: %w", sha, err)
		}
		accumulateCommit(&res, files)
		res.Commits++
	}

	merged := prev
	merged.Bytes = make(map[string]int, len(prev.Bytes)+len(res.Bytes))
	for k, v := range prev.Bytes {
		merged.Bytes[k] = v
	}
	for k, v := range res.Bytes {
		merged.Bytes[k] += v
	}
	merged.Commits += res.Commits
	merged.Files += res.Files
	merged.Lines += res.Lines
	if newHead := compareHeadSHA(cmp); newHead != "" {
		merged.HeadSHA = newHead
	}
	return merged, foldApplied, nil
}

func compareHeadSHA(cmp *github.CommitsComparison) string {
	if n := len(cmp.Commits); n > 0 {
		return cmp.Commits[n-1].GetSHA()
	}
	return ""
}

func accumulateCommit(res *walkResult, files []*github.CommitFile) {
	for _, f := range files {
		added := f.GetAdditions()
		path := f.GetFilename()
		if added <= 0 || path == "" {
			continue
		}
		if excludedPath(path) || enry.IsVendor(path) || enry.IsDocumentation(path) || enry.IsGenerated(path, nil) {
			continue
		}
		lang := classifyPath(path)
		if lang == "" {
			continue
		}
		switch enry.GetLanguageType(lang) {
		case enry.Programming, enry.Markup:
		default:
			continue
		}
		res.Bytes[lang] += added
		res.Files++
		res.Lines += added
	}
}

func buildSearchQuery(predicate string) string {
	if strings.Contains(predicate, "@") {
		return "author-email:" + predicate
	}
	return "author:" + predicate
}

func splitFullName(s string) (owner, name string) {
	i := strings.IndexByte(s, '/')
	if i < 0 {
		return "", s
	}
	return s[:i], s[i+1:]
}

func resolveRepo(
	t repoTask,
	prev repoEntry,
	hit bool,
	compute func() (walkResult, string, error),
	fold func(repoEntry) (repoEntry, foldOutcome, error),
) (repoEntry, string, error) {
	recompute := func() (repoEntry, string, error) {
		res, source, err := compute()
		if err != nil {
			return repoEntry{}, source, err
		}
		return repoEntry{
			HeadSHA:  res.HeadSHA,
			PushedAt: t.PushedAt,
			Bytes:    res.Bytes,
			Commits:  res.Commits,
			Files:    res.Files,
			Lines:    res.Lines,
		}, source, nil
	}
	if !hit {
		return recompute()
	}
	if t.PushedAt != "" && t.PushedAt == prev.PushedAt {
		return prev, "cache", nil
	}
	if prev.HeadSHA == "" || t.DefaultBranch == "" {
		return recompute()
	}
	folded, outcome, err := fold(prev)
	if err != nil || outcome == foldRecompute {
		return recompute()
	}
	folded.PushedAt = t.PushedAt
	return folded, "fold", nil
}

func resolveHeadSHA(ctx context.Context, env *plugin.Env, owner, name, branch string) string {
	if env.REST == nil || branch == "" {
		return ""
	}
	b, _, err := env.REST.Repositories.GetBranch(ctx, owner, name, branch, 1)
	if err != nil {
		return ""
	}
	return b.GetCommit().GetSHA()
}
