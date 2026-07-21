package languages

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	gitcmd "github.com/gogs/git-module"
	"github.com/google/go-github/v66/github"
	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/require"
	"github.com/twangodev/gmetrics/internal/plugin"
)

func TestSelectAuthoredSHAs(t *testing.T) {
	preds := []string{"octocat"}
	commits := []*github.RepositoryCommit{
		{SHA: github.String("a"), Commit: &github.Commit{Author: &github.CommitAuthor{
			Name: github.String("Octocat"), Email: github.String("octo@x")}}},
		{SHA: github.String("b"), Commit: &github.Commit{Author: &github.CommitAuthor{
			Name: github.String("Someone Else"), Email: github.String("nope@x")}}},
		{SHA: github.String("m"), Parents: []*github.Commit{{}, {}}, Commit: &github.Commit{Author: &github.CommitAuthor{
			Name: github.String("Octocat"), Email: github.String("octo@x")}}},
	}
	got := selectAuthoredSHAs(commits, preds)
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("want [a] (merge commit m excluded), got %v", got)
	}
}

func TestRepoTaskCarriesWatermarkFields(t *testing.T) {
	var rt repoTask
	rt.PushedAt = "2026-05-26T00:00:00Z"
	rt.DefaultBranch = "main"
	if rt.PushedAt == "" || rt.DefaultBranch == "" {
		t.Fatal("repoTask must carry PushedAt and DefaultBranch")
	}
}

func TestAccumulateCommitFiltersAndSums(t *testing.T) {
	res := walkResult{Bytes: map[string]int{}}
	files := []*github.CommitFile{
		{Filename: github.String("main.go"), Additions: github.Int(5)},
		{Filename: github.String("vendor/x/y.go"), Additions: github.Int(99)},
		{Filename: github.String("README.md"), Additions: github.Int(3)},
		{Filename: github.String("bin.png"), Additions: github.Int(0)},
	}
	accumulateCommit(&res, files)
	if res.Bytes["Go"] != 5 {
		t.Fatalf("want Go=5, got %d", res.Bytes["Go"])
	}
	if _, ok := res.Bytes["Markdown"]; ok {
		t.Fatal("documentation file should be excluded")
	}
	if res.Files != 1 || res.Lines != 5 {
		t.Fatalf("want files=1 lines=5, got files=%d lines=%d", res.Files, res.Lines)
	}
}

func TestBuildSearchQuery(t *testing.T) {
	if got := buildSearchQuery("octocat"); got != "author:octocat" {
		t.Fatalf("want author:octocat, got %q", got)
	}
	if got := buildSearchQuery("octo@users.noreply.github.com"); got != "author-email:octo@users.noreply.github.com" {
		t.Fatalf("want author-email:..., got %q", got)
	}
}

func TestBuildAuthorPredicates_AndMatch(t *testing.T) {
	t.Parallel()

	env := &plugin.Env{
		Login: "twangodev",
		User: plugin.UserContext{
			Login:      "twangodev",
			DatabaseID: 48845764,
		},
	}

	cfg := Config{
		CommitsAuthoring: []string{
			".user.login",
			"james@fish.audio",
			"otheralias",
			"  ",
			"",
		},
	}

	preds := buildAuthorPredicates(context.Background(), env, cfg)
	require.Len(t, preds, 5, "blank entries must be filtered; .user.login expands to login + 2 noreply forms")

	cases := []struct {
		name, an, ae string
		want         bool
	}{
		{"github noreply with db-id prefix matches .user.login", "James", "48845764+twangodev@users.noreply.github.com", true},
		{"github noreply short form matches .user.login", "James", "twangodev@users.noreply.github.com", true},
		{"author name containing login matches .user.login (case-insensitive)", "TwangoDev", "anything@example.com", true},
		{"literal email matches (case-insensitive)", "James Ding", "James@Fish.Audio", true},
		{"bare login matches author name (case-insensitive)", "OtherAlias", "unrelated@example.com", true},
		{"unrelated commit does not match", "Random Person", "rando@example.com", false},
		{"noreply for a different login does not match", "Random Person", "12345+somebodyelse@users.noreply.github.com", false},
		{"different mailbox at same domain does not match email predicate", "Random", "other@fish.audio", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, authorMatches(preds, tc.an, tc.ae))
		})
	}
}

func TestBuildAuthorPredicates_NoLogin(t *testing.T) {
	t.Parallel()

	env := &plugin.Env{}
	cfg := Config{CommitsAuthoring: []string{".user.login", "user@example.com"}}

	preds := buildAuthorPredicates(context.Background(), env, cfg)
	require.Len(t, preds, 1, ".user.login must be dropped when login is unknown")
	require.Equal(t, "user@example.com", preds[0])
}

func TestBuildAuthorPredicates_NoDatabaseID(t *testing.T) {
	t.Parallel()

	env := &plugin.Env{
		Login: "alice",
		User:  plugin.UserContext{Login: "alice"},
	}
	cfg := Config{CommitsAuthoring: []string{".user.login"}}

	preds := buildAuthorPredicates(context.Background(), env, cfg)
	require.Len(t, preds, 2, "login + short noreply; no <id>+<login> form without db id")
	require.Contains(t, preds, "alice")
	require.Contains(t, preds, "alice@users.noreply.github.com")

	require.True(t, authorMatches(preds, "irrelevant", "alice@users.noreply.github.com"))
	// Upstream substring semantics: a bogus "0+alice" form would still match the bare login; not defended against.
}

func TestGitEnvWithTokenUsesHeaderInsteadOfCloneURL(t *testing.T) {
	t.Parallel()

	const token = "secret-token"
	env := gitEnvWithToken(token)
	require.Contains(t, env, "GIT_CONFIG_COUNT=3")
	require.Contains(t, env, "GIT_CONFIG_KEY_2=http.extraHeader")
	require.Contains(t, env, "GIT_CONFIG_VALUE_2=Authorization: Basic "+
		base64.StdEncoding.EncodeToString([]byte("oauth2:"+token)))
	for _, value := range env {
		require.NotContains(t, value, token)
	}
}

func TestGitEnvWithTokenSendsAuthorizationHeader(t *testing.T) {
	authorization := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authorization <- req.Header.Get("Authorization")
		http.Error(w, "fixture stops after header capture", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	_, err := gitcmd.NewCommand("ls-remote", srv.URL+"/private.git").
		AddOptions(gitcmd.CommandOptions{
			Context: context.Background(),
			Envs:    gitEnvWithToken("secret-token"),
			Timeout: noTimeout,
		}).Run()
	require.Error(t, err)
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("oauth2:secret-token")), <-authorization)
}

func TestResolveRepoReusesWhenPushedAtUnchanged(t *testing.T) {
	prev := repoEntry{HeadSHA: "h1", PushedAt: "t1", Bytes: map[string]int{"Go": 7}, Commits: 1, Files: 1, Lines: 7}
	called := false
	compute := func() (walkResult, string, error) {
		called = true
		return walkResult{}, "clone", nil
	}
	fold := func(base repoEntry) (repoEntry, foldOutcome, error) {
		t.Fatal("fold must not run when pushed_at is unchanged")
		return base, foldApplied, nil
	}
	got, _, err := resolveRepo(repoTask{FullName: "o/r", PushedAt: "t1"}, prev, true, compute, fold)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("compute must not run on cache hit")
	}
	if got.Bytes["Go"] != 7 {
		t.Fatalf("want reused entry, got %+v", got)
	}
}

func TestResolveRepoFoldsWhenChanged(t *testing.T) {
	prev := repoEntry{HeadSHA: "h1", PushedAt: "t1", Bytes: map[string]int{"Go": 7}, Commits: 1}
	fold := func(base repoEntry) (repoEntry, foldOutcome, error) {
		base.Bytes = map[string]int{"Go": 10}
		base.PushedAt = "t2"
		return base, foldApplied, nil
	}
	got, _, err := resolveRepo(repoTask{FullName: "o/r", PushedAt: "t2", DefaultBranch: "main"}, prev, true,
		func() (walkResult, string, error) {
			t.Fatal("should fold, not recompute")
			return walkResult{}, "", nil
		},
		fold)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bytes["Go"] != 10 || got.PushedAt != "t2" {
		t.Fatalf("want folded entry, got %+v", got)
	}
}

func TestResolveRepoRecomputesOnFoldRecompute(t *testing.T) {
	prev := repoEntry{HeadSHA: "h1", PushedAt: "t1"}
	computed := false
	compute := func() (walkResult, string, error) {
		computed = true
		return walkResult{Bytes: map[string]int{"Rust": 4}, Commits: 1, Files: 1, Lines: 4}, "clone", nil
	}
	fold := func(base repoEntry) (repoEntry, foldOutcome, error) {
		return base, foldRecompute, nil
	}
	got, _, err := resolveRepo(repoTask{FullName: "o/r", PushedAt: "t2", DefaultBranch: "main"}, prev, true, compute, fold)
	if err != nil {
		t.Fatal(err)
	}
	if !computed {
		t.Fatal("recompute must run when fold returns foldRecompute")
	}
	if got.Bytes["Rust"] != 4 {
		t.Fatalf("want recomputed entry, got %+v", got)
	}
}

func TestResolveRepoRecomputesOnFoldError(t *testing.T) {
	computed := false
	compute := func() (walkResult, string, error) {
		computed = true
		return walkResult{Bytes: map[string]int{"Go": 11}, Lines: 11}, "clone", nil
	}
	fold := func(base repoEntry) (repoEntry, foldOutcome, error) {
		return base, foldRecompute, fmt.Errorf("commit detail unavailable")
	}

	got, source, err := resolveRepo(
		repoTask{FullName: "o/r", PushedAt: "t2", DefaultBranch: "main"},
		repoEntry{HeadSHA: "old", PushedAt: "t1", Bytes: map[string]int{"Go": 3}},
		true,
		compute,
		fold,
	)
	require.NoError(t, err)
	require.True(t, computed)
	require.Equal(t, "clone", source)
	require.Equal(t, 11, got.Lines)
}

func TestResolveRepoComputesOnCacheMiss(t *testing.T) {
	compute := func() (walkResult, string, error) {
		return walkResult{Bytes: map[string]int{"Go": 2}, Commits: 1, Files: 1, Lines: 2}, "clone", nil
	}
	fold := func(base repoEntry) (repoEntry, foldOutcome, error) {
		t.Fatal("no fold on cache miss")
		return base, foldApplied, nil
	}
	got, _, err := resolveRepo(repoTask{FullName: "o/r", PushedAt: "t1"}, repoEntry{}, false, compute, fold)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bytes["Go"] != 2 || got.PushedAt != "t1" {
		t.Fatalf("want computed entry, got %+v", got)
	}
}

func TestResolveRepoRecomputesWhenFoldInputsMissing(t *testing.T) {
	compute := func() (walkResult, string, error) {
		return walkResult{Bytes: map[string]int{"Go": 9}, Commits: 1}, "clone", nil
	}
	fold := func(base repoEntry) (repoEntry, foldOutcome, error) {
		t.Fatal("fold must not run without a base HeadSHA or default branch")
		return base, foldApplied, nil
	}
	prev := repoEntry{PushedAt: "t1", Bytes: map[string]int{"Go": 1}}
	got, _, err := resolveRepo(repoTask{FullName: "o/r", PushedAt: "t2", DefaultBranch: "main"}, prev, true, compute, fold)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bytes["Go"] != 9 {
		t.Fatalf("want recomputed entry, got %+v", got)
	}
}

func TestResolveRepoUsesComputedHeadSHA(t *testing.T) {
	compute := func() (walkResult, string, error) {
		return walkResult{Bytes: map[string]int{"Go": 1}, HeadSHA: "tip123"}, "clone", nil
	}
	fold := func(base repoEntry) (repoEntry, foldOutcome, error) {
		t.Fatal("no fold")
		return base, foldApplied, nil
	}
	got, _, err := resolveRepo(repoTask{FullName: "o/r", PushedAt: "t1"}, repoEntry{}, false, compute, fold)
	if err != nil {
		t.Fatal(err)
	}
	if got.HeadSHA != "tip123" {
		t.Fatalf("want HeadSHA tip123, got %q", got.HeadSHA)
	}
}

func TestWalkRepoCapturesHeadSHA(t *testing.T) {
	if _, err := gitcmd.BinVersion(); err != nil {
		t.Skip("git binary not available")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		out, err := gitcmd.NewCommand(args...).RunInDir(dir)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q")
	run("config", "user.email", "dev@example.com")
	run("config", "user.name", "Dev")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(dir+"/main.go", []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "main.go")
	run("commit", "-q", "-m", "init")
	want := run("rev-parse", "HEAD")

	res, err := walkRepo(context.Background(), dir, []string{"dev@example.com"})
	if err != nil {
		t.Fatalf("walkRepo: %v", err)
	}
	if res.HeadSHA != want {
		t.Fatalf("want HeadSHA %q, got %q", want, res.HeadSHA)
	}
}

func TestResolveRepoHEADEmptyRepository(t *testing.T) {
	dir := t.TempDir()
	_, err := gitcmd.NewCommand("init", "-q").RunInDir(dir)
	require.NoError(t, err)

	sha, empty, err := resolveRepoHEAD(context.Background(), dir)
	require.NoError(t, err)
	require.Empty(t, sha)
	require.True(t, empty)
}

func TestResolveRepoHEADDoesNotHideFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, empty, err := resolveRepoHEAD(ctx, t.TempDir())
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, empty)

	_, empty, err = resolveRepoHEAD(context.Background(), filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
	require.False(t, empty)
}

func TestWalkRepoBackfillMatchesFullWalk(t *testing.T) {
	version, err := gitcmd.BinVersion()
	if err != nil || !gitSupportsBackfill(version) {
		t.Skip("git backfill requires Git 2.49+")
	}

	dir := t.TempDir()
	run := func(args ...string) {
		out, err := gitcmd.NewCommand(args...).RunInDir(dir)
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "dev@example.com")
	run("config", "user.name", "Dev")
	run("config", "commit.gpgsign", "false")
	run("config", "uploadpack.allowFilter", "true")

	require.NoError(t, os.Mkdir(filepath.Join(dir, "data"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("fixture\n"), 0o644))
	for i := 0; i < 6; i++ {
		path := filepath.Join(dir, "data", fmt.Sprintf("%d.json", i))
		require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o644))
	}
	run("add", ".")
	run("commit", "-q", "-m", "authored code and data")

	run("config", "user.email", "other@example.com")
	run("config", "user.name", "Other")
	f, err := os.OpenFile(filepath.Join(dir, "main.go"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString("// not authored\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	run("commit", "-q", "-am", "other author")

	run("config", "user.email", "dev@example.com")
	run("config", "user.name", "Dev")
	require.NoError(t, os.Mkdir(filepath.Join(dir, "cmd"), 0o755))
	run("mv", "main.go", "cmd/main.go")
	f, err = os.OpenFile(filepath.Join(dir, "cmd", "main.go"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString("// authored after rename\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	run("commit", "-q", "-am", "authored rename")

	cloneURL := (&url.URL{Scheme: "file", Path: dir}).String()
	preds := []string{"dev@example.com"}
	full, err := walkRepoFull(context.Background(), cloneURL, preds)
	require.NoError(t, err)
	backfilled, err := walkRepoBackfill(context.Background(), cloneURL, preds)
	require.NoError(t, err)
	require.Equal(t, full.Bytes, backfilled.Bytes)
	require.Equal(t, full.Commits, backfilled.Commits)
	require.Equal(t, full.Files, backfilled.Files)
	require.Equal(t, full.Lines, backfilled.Lines)
}

func TestWalkRepoViaAPIListsCommitsByAuthor(t *testing.T) {
	var listCalls, detailCalls int
	var authorsMu sync.Mutex
	var authors []string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/commits", func(w http.ResponseWriter, req *http.Request) {
		listCalls++
		authorsMu.Lock()
		authors = append(authors, req.URL.Query().Get("author"))
		authorsMu.Unlock()
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"sha": "authored", "parents": []any{map[string]any{"sha": "parent"}}},
			map[string]any{"sha": "merge", "parents": []any{map[string]any{"sha": "p1"}, map[string]any{"sha": "p2"}}},
		})
	})
	mux.HandleFunc("/repos/o/r/commits/authored", func(w http.ResponseWriter, _ *http.Request) {
		detailCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sha":   "authored",
			"files": []any{map[string]any{"filename": "main.go", "additions": 7}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(srv.Client())
	baseURL, err := url.Parse(srv.URL + "/")
	require.NoError(t, err)
	client.BaseURL = baseURL
	client.UploadURL = baseURL

	res, err := walkRepoViaAPI(context.Background(), &plugin.Env{REST: client}, "o", "r", []string{"alice", "alice@example.com"})
	require.NoError(t, err)
	require.Equal(t, 2, listCalls)
	require.Equal(t, 1, detailCalls, "duplicate aliases must fetch a commit once and merges must be skipped")
	require.Equal(t, 1, res.Commits)
	require.Equal(t, 1, res.Files)
	require.Equal(t, 7, res.Lines)
	require.Equal(t, map[string]int{"Go": 7}, res.Bytes)
	authorsMu.Lock()
	defer authorsMu.Unlock()
	require.NotContains(t, authors, "", "each commit-list request must carry an author")
}

func TestWalkRepoViaAPIFailsOnMissingCommitDetail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/commits", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"sha": "broken", "parents": []any{map[string]any{"sha": "parent"}}},
		})
	})
	mux.HandleFunc("/repos/o/r/commits/broken", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary failure", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(srv.Client())
	baseURL, err := url.Parse(srv.URL + "/")
	require.NoError(t, err)
	client.BaseURL = baseURL
	client.UploadURL = baseURL

	res, err := walkRepoViaAPI(context.Background(), &plugin.Env{REST: client}, "o", "r", []string{"alice"})
	require.ErrorContains(t, err, "get commit broken")
	require.Zero(t, res.Files, "a partial API result must not be treated as complete")
}

func TestWalkRepoViaAPIFailsOnIncompleteCommitFilePagination(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/commits", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"sha": "large", "parents": []any{map[string]any{"sha": "parent"}}},
		})
	})
	mux.HandleFunc("/repos/o/r/commits/large", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("page") == "2" {
			http.Error(w, "second page unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/commits/large?page=2>; rel="next"`, requestOrigin(req)))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sha":   "large",
			"files": []any{map[string]any{"filename": "partial.go", "additions": 5}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(srv.Client())
	baseURL, err := url.Parse(srv.URL + "/")
	require.NoError(t, err)
	client.BaseURL = baseURL
	client.UploadURL = baseURL

	res, err := walkRepoViaAPI(context.Background(), &plugin.Env{REST: client}, "o", "r", []string{"alice"})
	require.ErrorContains(t, err, "get commit large")
	require.Zero(t, res.Files, "no page may be accumulated until the complete file list is available")
}

func requestOrigin(req *http.Request) string {
	return "http://" + req.Host
}

func TestWalkRepoViaCompareRecomputesOnMissingCommitDetail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/compare/base...main", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ahead",
			"ahead_by": 1,
			"commits": []any{map[string]any{
				"sha":     "broken",
				"parents": []any{map[string]any{"sha": "parent"}},
				"commit": map[string]any{"author": map[string]any{
					"name": "Alice", "email": "alice@example.com",
				}},
			}},
		})
	})
	mux.HandleFunc("/repos/o/r/commits/broken", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary failure", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(srv.Client())
	baseURL, err := url.Parse(srv.URL + "/")
	require.NoError(t, err)
	client.BaseURL = baseURL
	client.UploadURL = baseURL
	previous := repoEntry{HeadSHA: "base", Bytes: map[string]int{"Go": 7}, Lines: 7}

	got, outcome, err := walkRepoViaCompare(context.Background(), &plugin.Env{REST: client},
		"o", "r", "base", "main", []string{"alice@example.com"}, previous)
	require.ErrorContains(t, err, "get commit broken")
	require.Equal(t, foldRecompute, outcome)
	require.Equal(t, previous, got)
}

func TestBuildRepoTasksAppliesAffiliationsAndCombinedLimit(t *testing.T) {
	var queryBodies []string
	var affiliationsVariable any
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		queryBodies = append(queryBodies, payload.Query)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(payload.Query, "repositoriesContributedTo") {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"user": map[string]any{
				"repositoriesContributedTo": map[string]any{
					"nodes": []any{
						contributedRepositoryTaskNode("alice/two", "2026-01-02T00:00:00Z"),
						contributedRepositoryTaskNode("other/three", "2026-01-03T00:00:00Z"),
						contributedRepositoryTaskNode("other/four", "2026-01-04T00:00:00Z"),
					},
					"pageInfo": map[string]any{"endCursor": "", "hasNextPage": false},
				},
			}}})
			return
		}
		affiliationsVariable = payload.Variables["affiliations"]
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"user": map[string]any{
			"repositories": map[string]any{
				"nodes": []any{
					repositoryTaskNode("alice/one", "2026-01-01T00:00:00Z"),
					repositoryTaskNode("alice/two", "2026-01-02T00:00:00Z"),
				},
				"pageInfo": map[string]any{"endCursor": "", "hasNextPage": false},
			},
		}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	env := &plugin.Env{GraphQL: githubv4.NewEnterpriseClient(srv.URL+"/graphql", srv.Client())}
	cfg := defaultConfig()
	cfg.RepoMax = 3
	cfg.RepoAffiliations = []string{"owner", "collaborator"}
	tasks, err := buildRepoTasks(context.Background(), env, "alice", cfg)
	require.NoError(t, err)
	require.Len(t, queryBodies, 2)
	require.Equal(t, []any{"OWNER", "COLLABORATOR"}, affiliationsVariable)
	require.Equal(t, []string{"alice/one", "alice/two", "other/three"}, []string{
		tasks[0].FullName, tasks[1].FullName, tasks[2].FullName,
	})
	require.Equal(t, "affiliated", tasks[0].Source)
	require.Equal(t, "contributed", tasks[2].Source)
}

func repositoryTaskNode(name, pushedAt string) map[string]any {
	return map[string]any{
		"nameWithOwner": name,
		"url":           "https://github.com/" + name,
		"diskUsage":     10,
		"pushedAt":      pushedAt,
		"defaultBranchRef": map[string]any{
			"name": "main",
		},
	}
}

func contributedRepositoryTaskNode(name, pushedAt string) map[string]any {
	node := repositoryTaskNode(name, pushedAt)
	delete(node, "diskUsage")
	return node
}

func TestListUserReposUsesAuthenticatedEndpointForSelf(t *testing.T) {
	var repoPath, affiliations, visibility string
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "alice"})
	})
	mux.HandleFunc("/user/repos", func(w http.ResponseWriter, req *http.Request) {
		repoPath = req.URL.Path
		affiliations = req.URL.Query().Get("affiliation")
		visibility = req.URL.Query().Get("visibility")
		_ = json.NewEncoder(w).Encode([]any{map[string]any{
			"full_name": "alice/private", "clone_url": "https://github.com/alice/private.git",
			"private": true,
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(srv.Client())
	baseURL, err := url.Parse(srv.URL + "/")
	require.NoError(t, err)
	client.BaseURL = baseURL
	client.UploadURL = baseURL
	cfg := defaultConfig()
	cfg.RepoAffiliations = []string{"owner", "organization_member"}

	repos, err := listUserRepos(context.Background(), &plugin.Env{REST: client}, "alice", cfg)
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, "/user/repos", repoPath)
	require.Equal(t, "owner,organization_member", affiliations)
	require.Equal(t, "all", visibility)
}

func TestPublicRepoType(t *testing.T) {
	t.Parallel()
	require.Equal(t, "owner", publicRepoType(nil))
	require.Equal(t, "owner", publicRepoType([]string{"owner"}))
	require.Equal(t, "member", publicRepoType([]string{"collaborator", "organization_member"}))
	require.Equal(t, "all", publicRepoType([]string{"owner", "collaborator"}))
}

func TestIndepthStateConcurrentCheckpoints(t *testing.T) {
	const repositories = 16
	path := filepath.Join(t.TempDir(), "cache.json")
	state := newIndepthState(path, newCache("predicates"))

	var wg sync.WaitGroup
	errors := make(chan error, repositories)
	for i := 0; i < repositories; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("owner/repo-%02d", i)
			errors <- state.recordRepo(name, repoEntry{
				HeadSHA: name,
				Bytes:   map[string]int{"Go": i + 1},
				Commits: 1,
				Files:   1,
				Lines:   i + 1,
			}, true)
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}

	cache := loadCache(path)
	require.Len(t, cache.Repos, repositories)
	data := state.result(defaultConfig())
	require.Equal(t, repositories, data.IndepthCommits)
	require.Equal(t, repositories, data.IndepthFiles)
	require.Equal(t, repositories*(repositories+1)/2, data.IndepthLines)
}

func TestGitSupportsBackfill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		want    bool
	}{
		{version: "2.48.1", want: false},
		{version: "2.49.0", want: true},
		{version: "2.52.0", want: true},
		{version: "3.0.0", want: true},
		{version: "2.49.0.windows.1", want: true},
		{version: "unknown", want: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.version, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, gitSupportsBackfill(test.version))
		})
	}
}

func TestUseSelectiveBackfill(t *testing.T) {
	t.Parallel()

	require.True(t, useSelectiveBackfill(backfillProbe{}))
	require.True(t, useSelectiveBackfill(backfillProbe{
		FileChanges: 40, EligibleChanges: 10, EligiblePaths: make([]string, 10),
	}))
	require.False(t, useSelectiveBackfill(backfillProbe{
		FileChanges: 40, EligibleChanges: 11, EligiblePaths: make([]string, 10),
	}))
	require.False(t, useSelectiveBackfill(backfillProbe{
		FileChanges: 40, EligibleChanges: 10, EligiblePaths: make([]string, selectiveBackfillMaxPaths+1),
	}))
}

func TestParseRawProbe(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		commitMarker,
		"\n:100644 100644 a b M", "main.go",
		"\n:100644 100644 a b M", "vendor/lib.go",
		"\n:100644 100644 a b M", "README.md",
		commitMarker,
		"\n:100644 100644 a b R100", "legacy/app.ts", "src/app.ts",
		"\n:100644 100644 a b M", "templates/index.html",
		"\n:100644 000000 a 0 D", "deleted.go",
	}, "\x00") + "\x00"

	probe, err := parseRawProbe(bufio.NewReader(strings.NewReader(raw)))
	require.NoError(t, err)
	require.Equal(t, 2, probe.Commits)
	require.Equal(t, 6, probe.FileChanges)
	require.Equal(t, 3, probe.EligibleChanges)
	require.Equal(t, []string{
		"legacy/app.ts",
		"main.go",
		"src/app.ts",
		"templates/index.html",
	}, probe.EligiblePaths)
}
