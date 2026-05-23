package base

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shurcooL/githubv4"
	"github.com/twangodev/gmetrics/internal/plugin"
)

var errInvalidData = errors.New("base: render data has unexpected type")

// baseQuery is the single GraphQL query the base plugin issues against the
// GitHub API. It fetches the user's profile, follower/following counts, the
// repositories aggregate (filtered by ownerAffiliations), and the
// contributions collection for the trailing year together with the
// per-day contribution calendar.
//
// Repositories.OwnerAffiliations and the contribution-calendar date window
// are bound via the variables map passed to client.Query.
type baseQuery struct {
	User struct {
		Login      githubv4.String
		Name       githubv4.String
		AvatarURL  githubv4.String `graphql:"avatarUrl"`
		Bio        githubv4.String
		CreatedAt  githubv4.DateTime
		DatabaseID githubv4.Int `graphql:"databaseId"`

		Followers struct {
			TotalCount githubv4.Int
		}
		Following struct {
			TotalCount githubv4.Int
		}

		IssueComments struct {
			TotalCount githubv4.Int
		}
		Organizations struct {
			TotalCount githubv4.Int
		}
		StarredRepositories struct {
			TotalCount githubv4.Int
		}
		Watching struct {
			TotalCount githubv4.Int
		}
		SponsorshipsAsSponsor struct {
			TotalCount githubv4.Int
		}

		Repositories struct {
			TotalCount     githubv4.Int
			TotalDiskUsage githubv4.Int
		} `graphql:"repositories(first: 0, ownerAffiliations: $affiliations)"`

		ContributionsCollection struct {
			TotalCommitContributions            githubv4.Int
			TotalPullRequestContributions       githubv4.Int
			TotalPullRequestReviewContributions githubv4.Int
			TotalIssueContributions             githubv4.Int

			ContributionCalendar struct {
				Weeks []struct {
					ContributionDays []struct {
						ContributionCount githubv4.Int
						Date              githubv4.String
					}
				}
			}
		}
	} `graphql:"user(login: $login)"`
}

// Fetch performs the GraphQL query and assembles the plugin's Data value.
// Failed sub-queries surface as a returned error; partial data is not
// reported back to the engine in v1 (the engine wraps the error into an
// ErrorFragment).
func Fetch(ctx context.Context, env *plugin.Env, cfg Config) (Data, error) {
	var d Data
	d.Sections = append(d.Sections, cfg.Sections...)
	d.Hireable = cfg.Hireable
	d.Metadata.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	if env == nil || env.GraphQL == nil {
		return d, fmt.Errorf("base: env.GraphQL client is required")
	}

	login := env.Login
	if login == "" {
		login = env.User.Login
	}
	if login == "" {
		return d, fmt.Errorf("base: env.Login is empty")
	}

	affiliations := toRepositoryAffiliations(cfg.Repos.Affiliations)
	if len(affiliations) == 0 {
		affiliations = []githubv4.RepositoryAffiliation{githubv4.RepositoryAffiliationOwner}
	}

	vars := map[string]any{
		"login":        githubv4.String(login),
		"affiliations": affiliations,
	}

	var q baseQuery
	if err := env.GraphQL.Query(ctx, &q, vars); err != nil {
		return d, fmt.Errorf("base: graphql query: %w", err)
	}

	d.User = plugin.UserContext{
		Login:      string(q.User.Login),
		Name:       string(q.User.Name),
		AvatarURL:  string(q.User.AvatarURL),
		Bio:        string(q.User.Bio),
		CreatedAt:  q.User.CreatedAt.Time,
		Followers:  int(q.User.Followers.TotalCount),
		Following:  int(q.User.Following.TotalCount),
		DatabaseID: int64(q.User.DatabaseID),
	}
	if d.User.Login == "" {
		d.User.Login = login
	}

	d.Activity = Activity{
		Commits:      int(q.User.ContributionsCollection.TotalCommitContributions),
		PRsOpened:    int(q.User.ContributionsCollection.TotalPullRequestContributions),
		PRsReviewed:  int(q.User.ContributionsCollection.TotalPullRequestReviewContributions),
		IssuesOpened: int(q.User.ContributionsCollection.TotalIssueContributions),
		Comments:     int(q.User.IssueComments.TotalCount),
	}

	d.Community = Community{
		Orgs:      int(q.User.Organizations.TotalCount),
		Following: int(q.User.Following.TotalCount),
		Sponsors:  int(q.User.SponsorshipsAsSponsor.TotalCount),
		Stars:     int(q.User.StarredRepositories.TotalCount),
		Watching:  int(q.User.Watching.TotalCount),
	}

	d.Repositories = Repositories{
		Count: int(q.User.Repositories.TotalCount),
		Disk:  int(q.User.Repositories.TotalDiskUsage),
		// Releases, Packages, Forks, Watchers, Stargazers require additional
		// REST/GraphQL queries that are stretch goals for v1. Leave zero.
	}

	d.Calendar = lastNDays(q, 14)

	return d, nil
}

// toRepositoryAffiliations maps user-facing affiliation strings (lowercase,
// snake_case) to the corresponding githubv4 enum constants. Unknown values
// are silently dropped; the caller falls back to OWNER when the result is
// empty.
func toRepositoryAffiliations(raw []string) []githubv4.RepositoryAffiliation {
	out := make([]githubv4.RepositoryAffiliation, 0, len(raw))
	for _, r := range raw {
		switch r {
		case "owner", "OWNER":
			out = append(out, githubv4.RepositoryAffiliationOwner)
		case "collaborator", "COLLABORATOR":
			out = append(out, githubv4.RepositoryAffiliationCollaborator)
		case "organization_member", "ORGANIZATION_MEMBER":
			out = append(out, githubv4.RepositoryAffiliationOrganizationMember)
		}
	}
	return out
}

// lastNDays flattens the (weeks → days) contribution calendar produced by
// GraphQL into the trailing-n-day window the renderer expects. Days are
// returned in chronological order; if the API returned fewer than n days,
// all available days are returned.
func lastNDays(q baseQuery, n int) []DayCount {
	type day struct {
		date  string
		count int
	}
	var flat []day
	for _, w := range q.User.ContributionsCollection.ContributionCalendar.Weeks {
		for _, d := range w.ContributionDays {
			flat = append(flat, day{date: string(d.Date), count: int(d.ContributionCount)})
		}
	}
	if n > len(flat) {
		n = len(flat)
	}
	out := make([]DayCount, 0, n)
	for _, d := range flat[len(flat)-n:] {
		out = append(out, DayCount{Date: d.date, Count: d.count})
	}
	return out
}
