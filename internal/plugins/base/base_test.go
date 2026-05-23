package base

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
