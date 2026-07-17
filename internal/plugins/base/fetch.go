package base

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v66/github"
	"github.com/shurcooL/githubv4"
	"github.com/twangodev/gmetrics/internal/img"
	"github.com/twangodev/gmetrics/internal/plugin"
)

var errInvalidData = errors.New("base: render data has unexpected type")

type baseProfileQuery struct {
	User struct {
		Login      githubv4.String
		Name       githubv4.String
		AvatarURL  githubv4.String `graphql:"avatarUrl"`
		Bio        githubv4.String
		CreatedAt  githubv4.DateTime
		DatabaseID githubv4.Int     `graphql:"databaseId"`
		IsHireable githubv4.Boolean `graphql:"isHireable"`

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

		RepositoriesContributedTo struct {
			TotalCount githubv4.Int
		} `graphql:"repositoriesContributedTo(first: 0, includeUserRepositories: false, contributionTypes: [COMMIT, PULL_REQUEST, ISSUE, REPOSITORY])"`
	} `graphql:"user(login: $login)"`
}

type baseContributionsQuery struct {
	User struct {
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

func Fetch(ctx context.Context, env *plugin.Env, cfg Config) (Data, error) {
	var d Data
	d.Sections = append(d.Sections, cfg.Sections...)
	d.Metadata.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	// No sections means a plugins-only card (base: '') can run with token: NOT_NEEDED.
	if len(cfg.Sections) == 0 {
		return d, nil
	}

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

	var profile baseProfileQuery
	if err := env.GraphQL.Query(ctx, &profile, vars); err != nil {
		return d, fmt.Errorf("base: profile graphql query: %w", err)
	}

	var contributions baseContributionsQuery
	if err := env.GraphQL.Query(ctx, &contributions, map[string]any{"login": githubv4.String(login)}); err != nil {
		return d, fmt.Errorf("base: contributions graphql query: %w", err)
	}

	d.User = plugin.UserContext{
		Login:      string(profile.User.Login),
		Name:       string(profile.User.Name),
		AvatarURL:  string(profile.User.AvatarURL),
		Bio:        string(profile.User.Bio),
		CreatedAt:  profile.User.CreatedAt.Time,
		Followers:  int(profile.User.Followers.TotalCount),
		Following:  int(profile.User.Following.TotalCount),
		DatabaseID: int64(profile.User.DatabaseID),
	}
	if d.User.Login == "" {
		d.User.Login = login
	}

	d.Hireable = cfg.Hireable && bool(profile.User.IsHireable)

	d.Activity = Activity{
		Commits:      int(contributions.User.ContributionsCollection.TotalCommitContributions),
		PRsOpened:    int(contributions.User.ContributionsCollection.TotalPullRequestContributions),
		PRsReviewed:  int(contributions.User.ContributionsCollection.TotalPullRequestReviewContributions),
		IssuesOpened: int(contributions.User.ContributionsCollection.TotalIssueContributions),
		Comments:     int(profile.User.IssueComments.TotalCount),
	}

	d.ContributedTo = int(profile.User.RepositoriesContributedTo.TotalCount)

	d.Community = Community{
		Orgs:      int(profile.User.Organizations.TotalCount),
		Following: int(profile.User.Following.TotalCount),
		Sponsors:  int(profile.User.SponsorshipsAsSponsor.TotalCount),
		Stars:     int(profile.User.StarredRepositories.TotalCount),
		Watching:  int(profile.User.Watching.TotalCount),
	}

	// Packages is left zero: GraphQL does not expose it for arbitrary users (needs a read:packages REST call).
	d.Repositories = Repositories{
		Count: int(profile.User.Repositories.TotalCount),
		Disk:  int(profile.User.Repositories.TotalDiskUsage),
	}

	if err := populateRepoStats(ctx, env, cfg, affiliations, login, &d); err != nil {
		if env.Log != nil {
			env.Log.Warn("base: repo stats aggregation failed", "err", err)
		}
	}

	if cfg.Indepth {
		if err := populateIndepthContributions(ctx, env, login, profile.User.CreatedAt.Time, &d); err != nil {
			if env.Log != nil {
				env.Log.Warn("base: indepth contributions aggregation failed", "err", err)
			}
		}
	}

	if len(cfg.CommitsAuthoring) > 0 && env.REST != nil {
		if err := populateAuthoredCommitCount(ctx, env, cfg, login, &d); err != nil {
			if env.Log != nil {
				env.Log.Warn("base: authored commit count failed", "err", err)
			}
		}
	}

	d.Calendar = lastNDays(contributions, 14)

	// Base64-embed the avatar so the SVG is self-contained and survives GitHub's Camo proxy.
	if env.HTTP != nil && d.User.AvatarURL != "" {
		if b64, err := img.FetchAvatar(ctx, env.HTTP, d.User.AvatarURL); err == nil {
			d.AvatarB64 = b64
		} else if env.Log != nil {
			env.Log.Warn("base: avatar fetch failed", "err", err)
		}
	}

	return d, nil
}

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

const defaultRepoBatch = 100

type repoStatsQuery struct {
	User struct {
		Repositories struct {
			Nodes []struct {
				ForkCount      githubv4.Int
				StargazerCount githubv4.Int
				IsFork         githubv4.Boolean
				Watchers       struct{ TotalCount githubv4.Int }
				Releases       struct{ TotalCount githubv4.Int }
				LicenseInfo    struct {
					SpdxID githubv4.String `graphql:"spdxId"`
				}
			}
			PageInfo struct {
				HasNextPage githubv4.Boolean
				EndCursor   githubv4.String
			}
		} `graphql:"repositories(first: $batch, after: $after, ownerAffiliations: $affiliations, isFork: $includeForks)"`
	} `graphql:"user(login: $login)"`
}

func populateRepoStats(
	ctx context.Context,
	env *plugin.Env,
	cfg Config,
	affiliations []githubv4.RepositoryAffiliation,
	login string,
	d *Data,
) error {
	if cfg.Repos.Max == 0 {
		return nil
	}

	batch := cfg.Repos.Batch
	if batch <= 0 {
		batch = defaultRepoBatch
	}
	if batch > cfg.Repos.Max {
		batch = cfg.Repos.Max
	}

	// Scope aggregates to OWNER only: counting collaborator/org-member repos would
	// attribute their stars/watchers to every member.
	ownerOnly := []githubv4.RepositoryAffiliation{githubv4.RepositoryAffiliationOwner}
	_ = affiliations // aggregates use ownerOnly, not the configured affiliations
	vars := map[string]any{
		"login":        githubv4.String(login),
		"affiliations": ownerOnly,
		"batch":        githubv4.Int(batch),
		"after":        (*githubv4.String)(nil),
		"includeForks": (*githubv4.Boolean)(nil),
	}
	// includeForks is a nullable Boolean: nil includes forks, false excludes them.
	if !cfg.Repos.Forks {
		vars["includeForks"] = githubv4.NewBoolean(githubv4.Boolean(false))
	}

	var (
		forks, stars, watchers, releases int
		seen                             int
		licenseCounts                    = map[string]int{}
		licenseOrder                     []string
	)

	// Bound iterations in case the API reports HasNextPage indefinitely.
	pageGuard := (cfg.Repos.Max/batch)*2 + 4

	for pageGuard > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		pageGuard--

		remaining := cfg.Repos.Max - seen
		if remaining <= 0 {
			break
		}
		if remaining < batch {
			vars["batch"] = githubv4.Int(remaining)
		} else {
			vars["batch"] = githubv4.Int(batch)
		}

		var q repoStatsQuery
		if err := env.GraphQL.Query(ctx, &q, vars); err != nil {
			return fmt.Errorf("base: repo stats graphql: %w", err)
		}

		for _, repo := range q.User.Repositories.Nodes {
			forks += int(repo.ForkCount)
			stars += int(repo.StargazerCount)
			watchers += int(repo.Watchers.TotalCount)
			releases += int(repo.Releases.TotalCount)
			if spdx := string(repo.LicenseInfo.SpdxID); spdx != "" {
				if _, ok := licenseCounts[spdx]; !ok {
					licenseOrder = append(licenseOrder, spdx)
				}
				licenseCounts[spdx]++
			}
			seen++
			if seen >= cfg.Repos.Max {
				break
			}
		}

		if seen >= cfg.Repos.Max {
			break
		}
		if !bool(q.User.Repositories.PageInfo.HasNextPage) {
			break
		}
		vars["after"] = githubv4.NewString(q.User.Repositories.PageInfo.EndCursor)
	}

	d.Repositories.Forks = forks
	d.Repositories.Stargazers = stars
	d.Repositories.Watchers = watchers
	d.Repositories.Releases = releases

	if len(licenseOrder) > 0 {
		mostCommonLicense := licenseOrder[0]
		mostCommonCount := licenseCounts[mostCommonLicense]
		for _, spdx := range licenseOrder[1:] {
			if licenseCounts[spdx] > mostCommonCount {
				mostCommonLicense = spdx
				mostCommonCount = licenseCounts[spdx]
			}
		}
		d.Repositories.License = mostCommonLicense
	}

	return nil
}

// $from and $to span at most 1 year, the GitHub API limit per query.
type indepthContribQuery struct {
	User struct {
		ContributionsCollection struct {
			TotalCommitContributions            githubv4.Int
			TotalPullRequestContributions       githubv4.Int
			TotalPullRequestReviewContributions githubv4.Int
			TotalIssueContributions             githubv4.Int
		} `graphql:"contributionsCollection(from: $from, to: $to)"`
	} `graphql:"user(login: $login)"`
}

// GitHub launched in 2008, so the oldest accounts span ~18-19 yearly windows.
const maxIndepthWindows = 20

func populateIndepthContributions(
	ctx context.Context,
	env *plugin.Env,
	login string,
	createdAt time.Time,
	d *Data,
) error {
	if createdAt.IsZero() {
		return fmt.Errorf("base: createdAt is zero, cannot compute indepth window")
	}

	var (
		commits     int
		prsOpened   int
		prsReviewed int
		issues      int
	)

	now := time.Now().UTC()
	to := now
	from := to.AddDate(-1, 0, 0)
	if from.Before(createdAt) {
		from = createdAt
	}

	for i := 0; i < maxIndepthWindows; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		vars := map[string]any{
			"login": githubv4.String(login),
			"from":  githubv4.DateTime{Time: from},
			"to":    githubv4.DateTime{Time: to},
		}

		var q indepthContribQuery
		if err := env.GraphQL.Query(ctx, &q, vars); err != nil {
			return fmt.Errorf("base: indepth graphql (from=%s to=%s): %w",
				from.Format(time.RFC3339), to.Format(time.RFC3339), err)
		}

		commits += int(q.User.ContributionsCollection.TotalCommitContributions)
		prsOpened += int(q.User.ContributionsCollection.TotalPullRequestContributions)
		prsReviewed += int(q.User.ContributionsCollection.TotalPullRequestReviewContributions)
		issues += int(q.User.ContributionsCollection.TotalIssueContributions)

		if !from.After(createdAt) {
			break
		}

		to = from
		from = to.AddDate(-1, 0, 0)
		if from.Before(createdAt) {
			from = createdAt
		}
	}

	d.Activity.Commits = commits
	d.Activity.PRsOpened = prsOpened
	d.Activity.PRsReviewed = prsReviewed
	d.Activity.IssuesOpened = issues

	return nil
}

// GitHub's commit-search endpoint allows 30 requests/minute; cap patterns to stay well under it.
const maxAuthoringPatterns = 5

func populateAuthoredCommitCount(
	ctx context.Context,
	env *plugin.Env,
	cfg Config,
	login string,
	d *Data,
) error {
	patterns := cfg.CommitsAuthoring
	if len(patterns) > maxAuthoringPatterns {
		if env.Log != nil {
			env.Log.Warn(
				"base: commits_authoring exceeds cap; truncating",
				"count", len(patterns),
				"cap", maxAuthoringPatterns,
			)
		}
		patterns = patterns[:maxAuthoringPatterns]
	}

	var (
		total      int
		queried    int
		firstErr   error
		loginLower = strings.ToLower(login)
	)

	for _, raw := range patterns {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		query := buildCommitSearchQuery(entry, login, loginLower)
		if query == "" {
			continue
		}

		result, _, err := env.REST.Search.Commits(ctx, query, &github.SearchOptions{
			ListOptions: github.ListOptions{PerPage: 1},
		})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("base: commit search %q: %w", query, err)
			}
			if env.Log != nil {
				env.Log.Warn(
					"base: commit search failed",
					"query", query,
					"err", err,
				)
			}
			continue
		}

		total += result.GetTotal()
		queried++
	}

	if queried == 0 {
		if firstErr != nil {
			return firstErr
		}
		return nil
	}

	d.Activity.AuthoredCommits = total
	d.Activity.Commits = total
	return nil
}

func buildCommitSearchQuery(entry, login, loginLower string) string {
	if entry == "" {
		return ""
	}
	trimmed := strings.TrimPrefix(entry, "@")
	matchesLogin := entry == ".user.login" || strings.EqualFold(trimmed, login) || strings.EqualFold(trimmed, loginLower)
	if matchesLogin {
		if login == "" {
			return ""
		}
		return "author:" + login
	}
	return "author-email:" + entry
}

func lastNDays(q baseContributionsQuery, n int) []DayCount {
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
