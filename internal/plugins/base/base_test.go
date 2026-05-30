package base

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twangodev/gmetrics/internal/githubapi"
	"github.com/twangodev/gmetrics/internal/plugin"
)

func TestRender_AllSections_NonEmpty(t *testing.T) {
	t.Parallel()

	data := Data{
		User: plugin.UserContext{
			Login: "octocat",
			Name:  "Mona Lisa Octocat",
		},
		Activity: Activity{
			Commits: 1200, PRsOpened: 42, PRsReviewed: 17, IssuesOpened: 11, Comments: 65,
		},
		Community: Community{
			Orgs: 3, Following: 89, Sponsors: 1, Stars: 230, Watching: 12,
		},
		Repositories: Repositories{
			Count: 50, Forks: 7, Watchers: 21, Stargazers: 340,
		},
		Metadata: Metadata{GeneratedAt: "2026-05-22T00:00:00Z"},
		Sections: []string{"header", "activity", "community", "repositories", "metadata"},
		Hireable: true,
	}

	frag, err := Render(nil, data)
	require.NoError(t, err)
	require.Equal(t, 440, frag.Width)
	require.Greater(t, frag.Height, 200)
	require.Contains(t, frag.Body, "<path")
	require.Contains(t, frag.Body, "<g class=\"plugin-base\">")
	// Sanity: the text-to-path conversion should produce many path elements,
	// not just one or two. A single character roughly emits one closed path;
	// the smoke test below assumes header + 12 stat rows generate at least
	// 20 path elements.
	require.GreaterOrEqual(t, strings.Count(frag.Body, "<path"), 20)
}

// TestFetch_EmptySections_SkipsGraphQL verifies base short-circuits before
// any GraphQL work when no sections are configured (base: ”). It passes a
// nil GraphQL client — which Fetch would otherwise reject — to prove the
// query is never attempted, so a plugins-only card runs with no GitHub token.
func TestFetch_EmptySections_SkipsGraphQL(t *testing.T) {
	t.Parallel()

	env := &plugin.Env{Login: "alice"} // GraphQL deliberately nil
	data, err := Fetch(context.Background(), env, Config{})
	require.NoError(t, err)
	require.Empty(t, data.Sections)
	require.Equal(t, plugin.UserContext{}, data.User, "no user should be fetched")
}

// TestRender_NoSections_EmptyFragment verifies base renders an empty-bodied
// fragment for an empty section list so the engine can omit it.
func TestRender_NoSections_EmptyFragment(t *testing.T) {
	t.Parallel()

	frag, err := Render(nil, Data{})
	require.NoError(t, err)
	require.Empty(t, frag.Body)
	require.Zero(t, frag.Height)
}

func TestFetch_PopulatesUser(t *testing.T) {
	t.Parallel()

	// A canned GraphQL response whose field shape matches the baseQuery
	// struct's serialization. Numeric counts deliberately differ between
	// sections so a mis-mapping shows up as a wrong field value.
	const respBody = `{
  "data": {
    "user": {
      "login": "alice",
      "name": "Alice",
      "avatarUrl": "https://example.test/alice.png",
      "bio": "hello",
      "createdAt": "2020-01-02T03:04:05Z",
      "databaseId": 4242,
      "followers": {"totalCount": 100},
      "following": {"totalCount": 50},
      "issueComments": {"totalCount": 7},
      "organizations": {"totalCount": 2},
      "starredRepositories": {"totalCount": 33},
      "watching": {"totalCount": 8},
      "sponsorshipsAsSponsor": {"totalCount": 1},
      "repositories": {"totalCount": 25, "totalDiskUsage": 9001},
      "contributionsCollection": {
        "totalCommitContributions": 1200,
        "totalPullRequestContributions": 30,
        "totalPullRequestReviewContributions": 12,
        "totalIssueContributions": 5,
        "contributionCalendar": {
          "weeks": [
            {"contributionDays": [
              {"contributionCount": 1, "date": "2026-05-19"},
              {"contributionCount": 2, "date": "2026-05-20"},
              {"contributionCount": 3, "date": "2026-05-21"},
              {"contributionCount": 4, "date": "2026-05-22"}
            ]}
          ]
        }
      }
    }
  }
}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)

	clients, err := githubapi.New(context.Background(), githubapi.Config{
		Token:          "ghp_dummy",
		GraphQLBaseURL: srv.URL + "/graphql",
	})
	require.NoError(t, err)

	env := &plugin.Env{
		Login:   "alice",
		REST:    clients.REST,
		GraphQL: clients.GraphQL,
	}
	cfg := defaultConfig()

	data, err := Fetch(context.Background(), env, cfg)
	require.NoError(t, err)

	require.Equal(t, "Alice", data.User.Name)
	require.Equal(t, "alice", data.User.Login)
	require.Equal(t, 100, data.User.Followers)
	require.Equal(t, 50, data.User.Following)
	require.Equal(t, int64(4242), data.User.DatabaseID)

	require.Equal(t, 1200, data.Activity.Commits)
	require.Equal(t, 30, data.Activity.PRsOpened)
	require.Equal(t, 12, data.Activity.PRsReviewed)
	require.Equal(t, 5, data.Activity.IssuesOpened)
	require.Equal(t, 7, data.Activity.Comments)

	require.Equal(t, 2, data.Community.Orgs)
	require.Equal(t, 50, data.Community.Following)
	require.Equal(t, 1, data.Community.Sponsors)
	require.Equal(t, 33, data.Community.Stars)
	require.Equal(t, 8, data.Community.Watching)

	require.Equal(t, 25, data.Repositories.Count)
	require.Equal(t, 9001, data.Repositories.Disk)

	require.Len(t, data.Calendar, 4)
	require.Equal(t, "2026-05-19", data.Calendar[0].Date)
	require.Equal(t, 4, data.Calendar[3].Count)

	require.Equal(t, []string{"header", "activity", "community", "repositories", "metadata"}, data.Sections)
	require.NotEmpty(t, data.Metadata.GeneratedAt)
}

// TestFetch_Hireable_TracksGitHub verifies the base_hireable input is a
// tracking switch, not a force-on switch: when enabled, the badge reflects
// the account's live GitHub "Available for hire" status; when disabled, the
// badge never shows regardless of GitHub.
func TestFetch_Hireable_TracksGitHub(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		track     bool // cfg.Hireable
		github    bool // GitHub's isHireable
		wantBadge bool
	}{
		{"track-and-github-hireable", true, true, true},
		{"track-but-github-not-hireable", true, false, false},
		{"not-tracking-even-if-github-hireable", false, true, false},
		{"not-tracking-and-not-hireable", false, false, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			respBody := fmt.Sprintf(`{
  "data": {
    "user": {
      "login": "alice",
      "isHireable": %t,
      "repositories": {"totalCount": 0, "totalDiskUsage": 0},
      "contributionsCollection": {"contributionCalendar": {"weeks": []}}
    }
  }
}`, tc.github)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(respBody))
			}))
			t.Cleanup(srv.Close)

			clients, err := githubapi.New(context.Background(), githubapi.Config{
				Token:          "ghp_dummy",
				GraphQLBaseURL: srv.URL + "/graphql",
			})
			require.NoError(t, err)

			env := &plugin.Env{Login: "alice", REST: clients.REST, GraphQL: clients.GraphQL}
			cfg := defaultConfig()
			cfg.Repos.Max = 0 // skip the repo-stats pagination; not under test
			cfg.Hireable = tc.track

			data, err := Fetch(context.Background(), env, cfg)
			require.NoError(t, err)
			require.Equal(t, tc.wantBadge, data.Hireable)
		})
	}
}

func TestBuildCommitSearchQuery(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		entry string
		login string
		want  string
	}{
		{"placeholder", ".user.login", "alice", "author:alice"},
		{"at-prefix-login", "@alice", "alice", "author:alice"},
		{"plain-login-match", "alice", "alice", "author:alice"},
		{"case-insensitive-login", "ALICE", "alice", "author:alice"},
		{"github-noreply-email", "12+alice@users.noreply.github.com", "alice", "author-email:12+alice@users.noreply.github.com"},
		{"plain-email", "james@fish.audio", "alice", "author-email:james@fish.audio"},
		{"empty-entry", "", "alice", ""},
		{"placeholder-no-login", ".user.login", "", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildCommitSearchQuery(tc.entry, tc.login, strings.ToLower(tc.login))
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPopulateAuthoredCommitCount_SumsAndReplaces(t *testing.T) {
	t.Parallel()

	// Stub server: returns total_count keyed off the q= query string.
	// Tracks the queries it received so we can assert each pattern was
	// translated correctly.
	var (
		mu      sync.Mutex
		queries []string
	)
	totals := map[string]int{
		"author:alice": 100,
		"author-email:12+alice@users.noreply.github.com": 200,
		"author-email:james@fish.audio":                  300,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		mu.Lock()
		queries = append(queries, q)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"total_count": %d, "incomplete_results": false, "items": []}`, totals[q])
	}))
	t.Cleanup(srv.Close)

	clients, err := githubapi.New(context.Background(), githubapi.Config{
		Token:       "ghp_dummy",
		RESTBaseURL: srv.URL + "/",
	})
	require.NoError(t, err)

	env := &plugin.Env{Login: "alice", REST: clients.REST}
	cfg := defaultConfig()
	cfg.CommitsAuthoring = []string{
		".user.login",
		"12+alice@users.noreply.github.com",
		"james@fish.audio",
	}

	d := Data{Activity: Activity{Commits: 50}}
	require.NoError(t, populateAuthoredCommitCount(context.Background(), env, cfg, "alice", &d))

	require.Equal(t, 600, d.Activity.AuthoredCommits)
	require.Equal(t, 600, d.Activity.Commits, "Commits should be replaced by the search total")

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, queries, 3)
	// Each query string is URL-decoded by net/url; assert the three expected forms.
	want := map[string]bool{
		"author:alice": true,
		"author-email:12+alice@users.noreply.github.com": true,
		"author-email:james@fish.audio":                  true,
	}
	for _, q := range queries {
		// net/url decodes "+" to space (form encoding). Restore the
		// literal "+" so the noreply-email query compares equal.
		q = strings.ReplaceAll(q, " ", "+")
		require.True(t, want[q], "unexpected query: %q", q)
	}
}

func TestPopulateAuthoredCommitCount_FailureLeavesCommitsUnchanged(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	clients, err := githubapi.New(context.Background(), githubapi.Config{
		Token:       "ghp_dummy",
		RESTBaseURL: srv.URL + "/",
	})
	require.NoError(t, err)

	env := &plugin.Env{Login: "alice", REST: clients.REST}
	cfg := defaultConfig()
	cfg.CommitsAuthoring = []string{".user.login"}

	d := Data{Activity: Activity{Commits: 77}}
	err = populateAuthoredCommitCount(context.Background(), env, cfg, "alice", &d)
	require.Error(t, err)
	require.Equal(t, 77, d.Activity.Commits, "Commits should be unchanged on failure")
	require.Equal(t, 0, d.Activity.AuthoredCommits)
}

func TestPopulateAuthoredCommitCount_CapsPatterns(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		callN int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		callN++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count": 1, "incomplete_results": false, "items": []}`))
	}))
	t.Cleanup(srv.Close)

	clients, err := githubapi.New(context.Background(), githubapi.Config{
		Token:       "ghp_dummy",
		RESTBaseURL: srv.URL + "/",
	})
	require.NoError(t, err)

	env := &plugin.Env{Login: "alice", REST: clients.REST}
	cfg := defaultConfig()
	// Seven patterns; cap is 5.
	cfg.CommitsAuthoring = []string{
		"a@example.com", "b@example.com", "c@example.com",
		"d@example.com", "e@example.com", "f@example.com",
		"g@example.com",
	}

	d := Data{}
	require.NoError(t, populateAuthoredCommitCount(context.Background(), env, cfg, "alice", &d))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, maxAuthoringPatterns, callN)
	require.Equal(t, maxAuthoringPatterns, d.Activity.AuthoredCommits)
}
