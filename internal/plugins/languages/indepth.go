package languages

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/debug"
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
	indepthConcurrency = 8
	noTimeout          = time.Duration(-1)
	apiThreshold       = 50
)

var perRepoBudget = 3 * time.Minute

var gitEnv = []string{"GIT_TERMINAL_PROMPT=0"}

type walkErrKind int

const (
	walkErrOther walkErrKind = iota
	walkErrNoAccess
	walkErrRateLimit
	walkErrTimeout
)

func authCloneURL(rawURL, token string) string {
	if token == "" {
		return rawURL
	}
	const prefix = "https://"
	if !strings.HasPrefix(rawURL, prefix) || strings.Contains(rawURL[len(prefix):], "@") {
		return rawURL
	}
	return prefix + "oauth2:" + token + "@" + rawURL[len(prefix):]
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
	var cacheMu sync.Mutex

	var mu sync.Mutex
	bytes := map[string]int{}
	var (
		done            int32
		totalAuthored   int64
		totalFiles      int64
		totalLines      int64
		skippedNoAccess int32
		failedRateLimit int32
	)
	startedAt := time.Now()
	quiet := os.Getenv("CI") == "true"

	var stopHeartbeat chan struct{}
	if quiet && env.Log != nil {
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
					n := atomic.LoadInt32(&done)
					env.Log.Info("languages: indepth progress",
						"completed", n, "total", len(tasks),
						"elapsed_s", int(time.Since(startedAt).Seconds()))
				}
			}
		}()
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(indepthConcurrency)

	total := len(tasks)
	for _, t := range tasks {
		t := t
		g.Go(func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					repo := t.FullName
					if quiet {
						repo = "***"
					}
					if env.Log != nil {
						env.Log.Error("languages: walk panicked", "repo", repo, "panic", r, "stack", string(debug.Stack()))
					}
					err = nil
				}
			}()
			return withRepoBudget(gctx, func(gctx context.Context) error {
				t0 := time.Now()
				owner, name := splitFullName(t.FullName)
				cacheMu.Lock()
				prev, hit := cache.Repos[t.FullName]
				cacheMu.Unlock()

				if env.Log != nil && !quiet {
					env.Log.Info("languages: walking",
						"repo", t.FullName, "source", t.Source, "size_kb", t.SizeKB)
				}

				entry, source, err := resolveRepo(t, prev, hit,
					func() (walkResult, string, error) { return walkTask(gctx, env, t, preds, login) },
					func(base repoEntry) (repoEntry, foldOutcome, error) {
						return walkRepoViaCompare(gctx, env, owner, name, base.HeadSHA, t.DefaultBranch, preds, base)
					},
				)
				n := atomic.AddInt32(&done, 1)
				if err != nil {
					kind, reason := classifyWalkErr(err)
					if kind == walkErrNoAccess {
						atomic.AddInt32(&skippedNoAccess, 1)
						return nil
					}
					if kind == walkErrRateLimit {
						atomic.AddInt32(&failedRateLimit, 1)
					}
					if env.Log != nil {
						repo := t.FullName
						if quiet {
							repo = "***"
						}
						env.Log.Warn("languages: walk failed",
							"repo", repo, "i", n, "total", total,
							"dur_ms", time.Since(t0).Milliseconds(), "reason", reason)
					}
					return nil
				}

				if source != "cache" && source != "fold" && entry.HeadSHA == "" {
					if h := resolveHeadSHA(gctx, env, owner, name, t.DefaultBranch); h != "" {
						entry.HeadSHA = h
					}
				}

				atomic.AddInt64(&totalAuthored, int64(entry.Commits))
				atomic.AddInt64(&totalFiles, int64(entry.Files))
				atomic.AddInt64(&totalLines, int64(entry.Lines))
				mu.Lock()
				for lang, b := range entry.Bytes {
					bytes[lang] += b
				}
				mu.Unlock()
				cacheMu.Lock()
				cache.Repos[t.FullName] = entry
				cacheMu.Unlock()

				if env.Log != nil && !quiet {
					env.Log.Info("languages: walked",
						"repo", t.FullName, "i", n, "total", total,
						"source", source, "dur_ms", time.Since(t0).Milliseconds(),
						"authored", entry.Commits, "langs", len(entry.Bytes))
				}
				return nil
			})
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

	seen := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		seen[t.FullName] = struct{}{}
	}
	cache.prune(seen)
	if err := saveCache(cfg.IndepthCachePath, cache); err != nil && env.Log != nil {
		env.Log.Warn("languages: cache save failed", "err", err)
	}

	if env.Log != nil {
		env.Log.Info("languages: indepth complete",
			"total_repos", total,
			"elapsed_s", int(time.Since(startedAt).Seconds()),
			"authored_commits", atomic.LoadInt64(&totalAuthored),
			"files", atomic.LoadInt64(&totalFiles),
			"lines", atomic.LoadInt64(&totalLines),
			"langs", len(bytes),
			"skipped_no_access", atomic.LoadInt32(&skippedNoAccess),
			"failed_rate_limit", atomic.LoadInt32(&failedRateLimit))
	}

	data := assemble(cfg, bytes, nil, true)
	data.IndepthCommits = int(atomic.LoadInt64(&totalAuthored))
	data.IndepthFiles = int(atomic.LoadInt64(&totalFiles))
	data.IndepthLines = int(atomic.LoadInt64(&totalLines))
	return data, nil
}

func listUserRepos(ctx context.Context, env *plugin.Env, login string, cfg Config) ([]*github.Repository, error) {
	cap := cfg.Limit * 5
	if cap < cfg.Limit {
		cap = cfg.Limit
	}
	opts := &github.RepositoryListOptions{
		Type:        "all",
		Sort:        "updated",
		ListOptions: github.ListOptions{PerPage: 50},
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
			Envs:    gitEnv,
			Timeout: noTimeout,
		},
	}); err != nil {
		return walkResult{}, fmt.Errorf("clone: %w", err)
	}
	cloneDur := time.Since(tClone)

	var headSHA string
	if out, rpErr := gitcmd.NewCommand("rev-parse", "HEAD").
		AddOptions(gitcmd.CommandOptions{Context: ctx, Envs: gitEnv, Timeout: noTimeout}).
		RunInDir(dir); rpErr == nil {
		headSHA = strings.TrimSpace(string(out))
	}

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
				Envs:    gitEnv,
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
	if enry.IsVendor(path) || enry.IsDocumentation(path) || enry.IsGenerated(path, nil) {
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
	owned, err := listUserRepos(ctx, env, login, cfg)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	tasks := make([]repoTask, 0, len(owned))
	for _, r := range owned {
		fn := r.GetFullName()
		if fn == "" || r.GetCloneURL() == "" {
			continue
		}
		seen[fn] = struct{}{}
		tasks = append(tasks, repoTask{
			FullName:      fn,
			CloneURL:      r.GetCloneURL(),
			SizeKB:        r.GetSize(),
			Source:        "owned",
			PushedAt:      r.GetPushedAt().UTC().Format(time.RFC3339),
			DefaultBranch: r.GetDefaultBranch(),
		})
	}

	contributed, err := listContributedRepos(ctx, env, login)
	if err != nil {
		if env.Log != nil {
			env.Log.Warn("languages: list contributed repos failed", "err", err)
		}
		return tasks, nil
	}
	for _, c := range contributed {
		if _, dup := seen[c.NameWithOwner]; dup {
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
	}
	return tasks, nil
}

func withRepoBudget(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, perRepoBudget)
	defer cancel()
	return fn(ctx)
}

func walkTask(ctx context.Context, env *plugin.Env, t repoTask, preds []string, login string) (walkResult, string, error) {
	owner, name := splitFullName(t.FullName)

	if t.Source == "contributed" && env.REST != nil {
		if count, err := probeAuthorCommitCount(ctx, env, owner, name, login); err == nil && count < apiThreshold {
			res, err := walkRepoViaAPI(ctx, env, owner, name, preds)
			return res, "api", err
		}
	}

	res, err := walkRepo(ctx, authCloneURL(t.CloneURL, env.Token), preds)
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

func listContributedRepos(ctx context.Context, env *plugin.Env, login string) ([]contribRepo, error) {
	if env.GraphQL == nil {
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
	for {
		if err := env.GraphQL.Query(ctx, &q, vars); err != nil {
			return nil, err
		}
		for _, n := range q.User.RepositoriesContributedTo.Nodes {
			out = append(out, contribRepo{
				NameWithOwner: string(n.NameWithOwner),
				CloneURL:      string(n.URL) + ".git",
				PushedAt:      n.PushedAt.UTC().Format(time.RFC3339),
				DefaultBranch: string(n.DefaultBranchRef.Name),
			})
		}
		if !q.User.RepositoriesContributedTo.PageInfo.HasNextPage {
			break
		}
		cursor := q.User.RepositoriesContributedTo.PageInfo.EndCursor
		vars["after"] = &cursor
	}
	return out, nil
}

func probeAuthorCommitCount(ctx context.Context, env *plugin.Env, owner, name, login string) (int, error) {
	query := fmt.Sprintf("author:%s repo:%s/%s", login, owner, name)
	res, _, err := env.REST.Search.Commits(ctx, query, &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: 1},
	})
	if err != nil {
		return 0, err
	}
	return res.GetTotal(), nil
}

func walkRepoViaAPI(ctx context.Context, env *plugin.Env, owner, name string, preds []string) (walkResult, error) {
	res := walkResult{Bytes: map[string]int{}}
	seen := map[string]struct{}{}

	for _, p := range preds {
		query := buildSearchQuery(p) + fmt.Sprintf(" repo:%s/%s", owner, name)
		opts := &github.SearchOptions{ListOptions: github.ListOptions{PerPage: 100}}
		for {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			sr, resp, err := env.REST.Search.Commits(ctx, query, opts)
			if err != nil {
				return res, err
			}
			for _, cr := range sr.Commits {
				seen[cr.GetSHA()] = struct{}{}
			}
			if resp == nil || resp.NextPage == 0 {
				break
			}
			opts.Page = resp.NextPage
		}
	}

	res.Commits = len(seen)
	for sha := range seen {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		commit, _, err := env.REST.Repositories.GetCommit(ctx, owner, name, sha, nil)
		if err != nil {
			continue
		}
		accumulateCommit(&res, commit.Files)
	}
	return res, nil
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
		commit, _, err := env.REST.Repositories.GetCommit(ctx, owner, name, sha, nil)
		if err != nil {
			continue
		}
		accumulateCommit(&res, commit.Files)
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
		if enry.IsVendor(path) || enry.IsDocumentation(path) || enry.IsGenerated(path, nil) {
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
