package languages

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twangodev/gmetrics/internal/plugin"
)

func TestWalkTaskHonorsPerRepoBudget(t *testing.T) {
	old := perRepoBudget
	perRepoBudget = 50 * time.Millisecond
	defer func() { perRepoBudget = old }()

	done := make(chan struct{})
	go func() {
		_, _, _ = withRepoBudget(context.Background(), func(ctx context.Context) (walkResult, string, error) {
			<-ctx.Done()
			return walkResult{}, "", ctx.Err()
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("withRepoBudget did not cancel at perRepoBudget")
	}
}

// TestBuildAuthorPredicates_AndMatch is a table-driven smoke test for the
// commits_authoring → predicate translation plus the per-commit matcher.
// It covers the three cases the languages plugin must handle, all using
// upstream's case-insensitive substring semantics on the `Name <email>`
// author header (mirroring `git log --author='<pat>' --regexp-ignore-case`):
//
//   - ".user.login" → expands into login + the two GitHub noreply email
//     forms;
//   - a literal email → matches anywhere in the author email/name field;
//   - a bare login → matches anywhere in the author email/name field.
//
// No real git or REST operations are performed; env.REST is nil so the
// GPG auto-discovery path is skipped.
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
			"  ", // blank entries should be dropped
			"",
		},
	}

	preds := buildAuthorPredicates(context.Background(), env, cfg)
	// .user.login expands to 3 (login + 2 noreply forms), plus 2 other
	// non-blank entries = 5 total. Blank entries are filtered.
	require.Len(t, preds, 5, "blank entries must be filtered; .user.login expands to 3")

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

// TestBuildAuthorPredicates_NoLogin verifies that .user.login degrades
// gracefully when the engine hasn't populated env.Login: the entry is
// silently dropped rather than producing a predicate that matches every
// commit (an empty substring would otherwise be a footgun).
func TestBuildAuthorPredicates_NoLogin(t *testing.T) {
	t.Parallel()

	env := &plugin.Env{} // no Login, no User, no REST
	cfg := Config{CommitsAuthoring: []string{".user.login", "user@example.com"}}

	preds := buildAuthorPredicates(context.Background(), env, cfg)
	require.Len(t, preds, 1, ".user.login must be dropped when login is unknown")
	require.Equal(t, "user@example.com", preds[0])
}

// TestBuildAuthorPredicates_NoDatabaseID verifies that when DatabaseID is
// zero we still expose the short noreply form (and not a bogus
// "0+login@users.noreply.github.com" entry).
func TestBuildAuthorPredicates_NoDatabaseID(t *testing.T) {
	t.Parallel()

	env := &plugin.Env{
		Login: "alice",
		User:  plugin.UserContext{Login: "alice"}, // DatabaseID == 0
	}
	cfg := Config{CommitsAuthoring: []string{".user.login"}}

	preds := buildAuthorPredicates(context.Background(), env, cfg)
	require.Len(t, preds, 2, "login + short noreply; no <id>+<login> form without db id")
	require.Contains(t, preds, "alice")
	require.Contains(t, preds, "alice@users.noreply.github.com")

	// And the short form actually matches.
	require.True(t, authorMatches(preds, "irrelevant", "alice@users.noreply.github.com"))
	// A bogus 0+alice form does technically contain the "alice" substring
	// so it'd match the bare login predicate — that's the upstream
	// substring behaviour and not something we try to defend against.
}
