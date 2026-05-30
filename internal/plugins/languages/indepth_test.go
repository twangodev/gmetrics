package languages

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	gitcmd "github.com/gogs/git-module"
	"github.com/google/go-github/v66/github"
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

func TestWalkTaskHonorsPerRepoBudget(t *testing.T) {
	old := perRepoBudget
	perRepoBudget = 50 * time.Millisecond
	defer func() { perRepoBudget = old }()

	done := make(chan struct{})
	go func() {
		_ = withRepoBudget(context.Background(), func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("withRepoBudget did not cancel at perRepoBudget")
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
