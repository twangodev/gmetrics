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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	enry "github.com/go-enry/go-enry/v2"
	gitcmd "github.com/gogs/git-module"
	"github.com/google/go-github/v66/github"
	"github.com/twangodev/gmetrics/internal/plugin"
	"golang.org/x/sync/errgroup"
)

const (
	indepthConcurrency = 8
	perRepoBudget      = 3 * time.Minute
	noTimeout          = time.Duration(-1)
)

var gitEnv = []string{"GIT_TERMINAL_PROMPT=0"}

// buildAuthorPredicates returns lower-cased substrings to pass as
// `--author=` patterns. ".user.login" expands into the bare login plus
// both GitHub noreply forms.
func buildAuthorPredicates(env *plugin.Env, cfg Config) []string {
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
		if entry == ".user.login" {
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

	return out
}

// authorMatches checks the case-insensitive `Name <Email>` header against
// every predicate as a substring. Kept for unit tests; the production
// walk delegates this to `git log --author=` directly.
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

	preds := buildAuthorPredicates(env, cfg)
	if env.Log != nil {
		env.Log.Info("languages: indepth author predicates", "count", len(preds))
	}
	if len(preds) == 0 {
		return Data{}, fmt.Errorf("languages: fetch indepth: no commits_authoring patterns configured")
	}

	if _, err := gitcmd.BinVersion(); err != nil {
		return Data{}, fmt.Errorf("languages: fetch indepth: git binary not available: %w", err)
	}

	if env.Log != nil {
		env.Log.Info("languages: indepth listing repos", "user", login)
	}
	repos, err := listUserRepos(ctx, env, login, cfg)
	if err != nil {
		return Data{}, fmt.Errorf("languages: fetch indepth: list repos: %w", err)
	}
	if env.Log != nil {
		env.Log.Info("languages: indepth cloning repos",
			"count", len(repos), "concurrency", indepthConcurrency)
	}

	var mu sync.Mutex
	bytes := map[string]int{}
	var (
		done          int32
		totalAuthored int64
		totalFiles    int64
		totalLines    int64
	)
	startedAt := time.Now()

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(indepthConcurrency)

	total := len(repos)
	for _, repo := range repos {
		repo := repo
		g.Go(func() error {
			cloneURL := repo.GetCloneURL()
			if cloneURL == "" {
				return nil
			}
			t0 := time.Now()
			if env.Log != nil {
				env.Log.Info("languages: cloning",
					"repo", repo.GetFullName(),
					"size_kb", repo.GetSize())
			}
			res, err := walkRepo(gctx, cloneURL, preds)
			n := atomic.AddInt32(&done, 1)
			if err != nil {
				if env.Log != nil {
					env.Log.Warn("languages: clone failed",
						"repo", repo.GetFullName(), "i", n, "total", total,
						"dur_ms", time.Since(t0).Milliseconds(), "err", err)
				}
				return nil
			}
			atomic.AddInt64(&totalAuthored, int64(res.Commits))
			atomic.AddInt64(&totalFiles, int64(res.Files))
			atomic.AddInt64(&totalLines, int64(res.Lines))
			mu.Lock()
			for name, n := range res.Bytes {
				bytes[name] += n
			}
			mu.Unlock()
			if env.Log != nil {
				env.Log.Info("languages: walked",
					"repo", repo.GetFullName(), "i", n, "total", total,
					"clone_ms", res.CloneDur.Milliseconds(),
					"log_ms", res.LogDur.Milliseconds(),
					"authored", res.Commits, "langs", len(res.Bytes))
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return Data{}, err
	}

	if env.Log != nil {
		env.Log.Info("languages: indepth complete",
			"total_repos", total,
			"elapsed_s", int(time.Since(startedAt).Seconds()),
			"authored_commits", atomic.LoadInt64(&totalAuthored),
			"files", atomic.LoadInt64(&totalFiles),
			"lines", atomic.LoadInt64(&totalLines),
			"langs", len(bytes))
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
	CloneDur time.Duration
	LogDur   time.Duration
}

const commitMarker = "__GMETRICS_COMMIT__"

func walkRepo(ctx context.Context, cloneURL string, preds []string) (walkResult, error) {
	ctx, cancel := context.WithTimeout(ctx, perRepoBudget)
	defer cancel()

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

	args := []string{
		"log",
		"--no-merges",
		"--numstat",
		"--regexp-ignore-case",
		// tformat: prefix is required so git treats the rest as literal,
		// not a named built-in format.
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
		// AddOptions overwrites Command.ctx, so the ctx must be passed
		// inside CommandOptions — not via NewCommandWithContext.
		runErrCh <- gitcmd.NewCommand(args...).
			AddOptions(gitcmd.CommandOptions{
				Context: ctx,
				Envs:    gitEnv,
				Timeout: noTimeout,
			}).
			RunInDirPipeline(pw, &stderrBuf, dir)
		_ = pw.Close()
	}()

	res := walkResult{Bytes: map[string]int{}, CloneDur: cloneDur}
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

// classifyNumstatLine parses one `<added>\t<deleted>\t<path>` line from
// `git log --numstat` and returns (language, addedLines, true) if the
// path is a non-vendored programming/markup file enry can identify.
func classifyNumstatLine(line string) (string, int, bool) {
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return "", 0, false
	}
	added, path := parts[0], parts[2]
	if added == "-" { // binary
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

// classifyPath returns enry's best-guess language for path using only
// the filename (GetLanguageByFilename catches full-name matches like
// Dockerfile; GetLanguages disambiguates extensions where the default
// candidate would otherwise be wrong, e.g. .md → Markdown not "GCC
// Machine Description").
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
