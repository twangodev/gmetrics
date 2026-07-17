package base

import (
	"context"
	"fmt"
	"io"
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
	// Header plus 12 stat rows convert to text-as-path glyphs; far more than this floor.
	const minGlyphPaths = 20
	require.GreaterOrEqual(t, strings.Count(frag.Body, "<path"), minGlyphPaths)
}

func TestFetch_EmptySections_SkipsGraphQL(t *testing.T) {
	t.Parallel()

	// Nil GraphQL client: Fetch would reject it if it ever attempted a query.
	envWithoutGraphQL := &plugin.Env{Login: "alice"}
	data, err := Fetch(context.Background(), envWithoutGraphQL, Config{})
	require.NoError(t, err)
	require.Empty(t, data.Sections)
	require.Equal(t, plugin.UserContext{}, data.User, "no user should be fetched")
}

func TestRender_NoSections_EmptyFragment(t *testing.T) {
	t.Parallel()

	frag, err := Render(nil, Data{})
	require.NoError(t, err)
	require.Empty(t, frag.Body)
	require.Zero(t, frag.Height)
}

func TestFetch_PopulatesUser(t *testing.T) {
	t.Parallel()

	var (
		mu          sync.Mutex
		queryBodies []string
	)

	// Counts differ per section so a field mis-mapping surfaces as a wrong value.
	const profileRespBody = `{
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
      "repositoriesContributedTo": {"totalCount": 6}
    }
  }
}`
	const contributionsRespBody = `{
  "data": {
    "user": {
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
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		queryBodies = append(queryBodies, string(body))
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(string(body), "contributionsCollection"):
			_, _ = w.Write([]byte(contributionsRespBody))
		case strings.Contains(string(body), "forkCount"):
			_, _ = w.Write([]byte(`{"data":{"user":{"repositories":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`))
		default:
			_, _ = w.Write([]byte(profileRespBody))
		}
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
	require.Equal(t, 6, data.ContributedTo)

	require.Len(t, data.Calendar, 4)
	require.Equal(t, "2026-05-19", data.Calendar[0].Date)
	require.Equal(t, 4, data.Calendar[3].Count)

	require.Equal(t, []string{"header", "activity", "community", "repositories", "metadata"}, data.Sections)
	require.NotEmpty(t, data.Metadata.GeneratedAt)

	mu.Lock()
	defer mu.Unlock()
	var profileQueries, contributionQueries int
	for _, body := range queryBodies {
		hasProfile := strings.Contains(body, "issueComments")
		hasContributions := strings.Contains(body, "contributionsCollection")
		require.False(t, hasProfile && hasContributions, "profile and contributions must not share a GraphQL query: %s", body)
		if hasProfile {
			profileQueries++
		}
		if hasContributions {
			contributionQueries++
		}
	}
	require.Equal(t, 1, profileQueries)
	require.Equal(t, 1, contributionQueries)
}

// base_hireable tracks GitHub's live "Available for hire" status; it never forces the badge on.
func TestFetch_Hireable_TracksGitHub(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		trackHireable  bool
		githubHireable bool
		wantBadge      bool
	}{
		{"track-and-github-hireable", true, true, true},
		{"track-but-github-not-hireable", true, false, false},
		{"not-tracking-even-if-github-hireable", false, true, false},
		{"not-tracking-and-not-hireable", false, false, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			profileRespBody := fmt.Sprintf(`{
  "data": {
    "user": {
      "login": "alice",
      "isHireable": %t,
      "repositories": {"totalCount": 0, "totalDiskUsage": 0}
    }
  }
}`, tc.githubHireable)
			const contributionsRespBody = `{"data":{"user":{"contributionsCollection":{"contributionCalendar":{"weeks":[]}}}}}`

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(string(body), "contributionsCollection") {
					_, _ = w.Write([]byte(contributionsRespBody))
					return
				}
				_, _ = w.Write([]byte(profileRespBody))
			}))
			t.Cleanup(srv.Close)

			clients, err := githubapi.New(context.Background(), githubapi.Config{
				Token:          "ghp_dummy",
				GraphQLBaseURL: srv.URL + "/graphql",
			})
			require.NoError(t, err)

			env := &plugin.Env{Login: "alice", REST: clients.REST, GraphQL: clients.GraphQL}
			cfg := defaultConfig()
			cfg.Repos.Max = 0 // skip repo-stats pagination; not under test
			cfg.Hireable = tc.trackHireable

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

	var (
		mu              sync.Mutex
		receivedQueries []string
	)
	totalsByQuery := map[string]int{
		"author:alice": 100,
		"author-email:12+alice@users.noreply.github.com": 200,
		"author-email:james@fish.audio":                  300,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		mu.Lock()
		receivedQueries = append(receivedQueries, q)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"total_count": %d, "incomplete_results": false, "items": []}`, totalsByQuery[q])
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
	require.Len(t, receivedQueries, 3)
	wantQueries := map[string]bool{
		"author:alice": true,
		"author-email:12+alice@users.noreply.github.com": true,
		"author-email:james@fish.audio":                  true,
	}
	for _, q := range receivedQueries {
		// net/url form-decodes "+" to space; restore it before comparing.
		q = strings.ReplaceAll(q, " ", "+")
		require.True(t, wantQueries[q], "unexpected query: %q", q)
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
		mu        sync.Mutex
		callCount int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		callCount++
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
	cfg.CommitsAuthoring = []string{
		"a@example.com", "b@example.com", "c@example.com",
		"d@example.com", "e@example.com", "f@example.com",
		"g@example.com",
	}
	require.Greater(t, len(cfg.CommitsAuthoring), maxAuthoringPatterns)

	d := Data{}
	require.NoError(t, populateAuthoredCommitCount(context.Background(), env, cfg, "alice", &d))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, maxAuthoringPatterns, callCount)
	require.Equal(t, maxAuthoringPatterns, d.Activity.AuthoredCommits)
}
