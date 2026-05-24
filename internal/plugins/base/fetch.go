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

		RepositoriesContributedTo struct {
			TotalCount githubv4.Int
		} `graphql:"repositoriesContributedTo(first: 0, includeUserRepositories: false, contributionTypes: [COMMIT, PULL_REQUEST, ISSUE, REPOSITORY])"`

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

	d.ContributedTo = int(q.User.RepositoriesContributedTo.TotalCount)

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
		// Packages is not exposed by GraphQL for arbitrary users (would need
		// a REST call to /users/:login/packages with read:packages scope);
		// leave it at zero. The remaining aggregates are populated below by
		// paginating the repositories collection.
	}

	// Aggregate per-repo stats (Forks, Stargazers, Watchers, Releases,
	// License) by walking the user's repositories. A failure here is
	// non-fatal: we log and leave the fields at zero so the rest of the
	// card still renders.
	if err := populateRepoStats(ctx, env, cfg, affiliations, login, &d); err != nil {
		if env.Log != nil {
			env.Log.Warn("base: repo stats aggregation failed", "err", err)
		}
	}

	// When indepth is enabled, replace the trailing-year activity counts
	// with totals summed across the user's entire GitHub lifetime via the
	// contributionsCollection's date-window form (max 1 year per query).
	// A failure here is non-fatal: we log and keep the last-year values.
	if cfg.Indepth {
		if err := populateIndepthContributions(ctx, env, login, q.User.CreatedAt.Time, &d); err != nil {
			if env.Log != nil {
				env.Log.Warn("base: indepth contributions aggregation failed", "err", err)
			}
		}
	}

	// When commits_authoring is set, replace d.Activity.Commits with the
	// sum of REST commit-search totals across the configured patterns.
	// This is more accurate than the GraphQL contributionsCollection count
	// for users whose commits are authored under multiple email identities
	// (e.g. github noreply + personal email). Non-fatal on failure: the
	// previous value of d.Activity.Commits is left in place.
	if len(cfg.CommitsAuthoring) > 0 && env.REST != nil {
		if err := populateAuthoredCommitCount(ctx, env, cfg, login, &d); err != nil {
			if env.Log != nil {
				env.Log.Warn("base: authored commit count failed", "err", err)
			}
		}
	}

	d.Calendar = lastNDays(q, 14)

	// Fetch and base64-embed the avatar so the SVG is self-contained and
	// survives GitHub's Camo proxy. Tests pass env.HTTP == nil to skip this.
	if env.HTTP != nil && d.User.AvatarURL != "" {
		if b64, err := img.FetchAvatar(ctx, env.HTTP, d.User.AvatarURL); err == nil {
			d.AvatarB64 = b64
		} else if env.Log != nil {
			env.Log.Warn("base: avatar fetch failed", "err", err)
		}
	}

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

// repoStatsQuery walks the user's repositories one page at a time to
// aggregate per-repo counters (forks, stars, watchers, releases) and the
// licenseInfo.spdxId distribution. The collection is filtered server-side
// by ownerAffiliations and (optionally) isFork; the includeForks flag is
// driven by cfg.Repos.Forks.
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

// populateRepoStats paginates over the user's repositories using
// cfg.Repos.Batch as the page size, capping the total number of repos
// inspected at cfg.Repos.Max. It mutates d.Repositories in place with the
// aggregated values. Returns nil if cfg.Repos.Max == 0 (caller opted out).
//
// License selection picks the most common spdxId across the inspected
// repos; ties are broken by first-encountered order (insertion order in
// the licenseOrder slice).
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
		batch = 100
	}
	if batch > cfg.Repos.Max {
		batch = cfg.Repos.Max
	}

	// For per-repo aggregates (stars / forks / watchers / releases /
	// license) we deliberately scope to OWNER only — including
	// collaborator/org-member repos would attribute their stars/watchers
	// to this user (e.g. a popular org repo with 1k stars would add 1k
	// stars per member). Upstream's classic template makes the same call.
	ownerOnly := []githubv4.RepositoryAffiliation{githubv4.RepositoryAffiliationOwner}
	_ = affiliations // affiliations is honored by the top-level counts; aggregates use ownerOnly
	vars := map[string]any{
		"login":        githubv4.String(login),
		"affiliations": ownerOnly,
		"batch":        githubv4.Int(batch),
		"after":        (*githubv4.String)(nil),
		"includeForks": (*githubv4.Boolean)(nil),
	}
	// includeForks is a nullable Boolean: nil means "no filter" (include
	// forks), false means "exclude forks". The default upstream behavior
	// excludes forks unless cfg.Repos.Forks is true.
	if !cfg.Repos.Forks {
		vars["includeForks"] = githubv4.NewBoolean(githubv4.Boolean(false))
	}

	var (
		forks, stars, watchers, releases int
		seen                             int
		licenseCounts                    = map[string]int{}
		licenseOrder                     []string
	)

	// pageGuard protects against an API that reports HasNextPage
	// indefinitely. cfg.Repos.Max / batch rounded up is the natural cap;
	// add a small fudge factor in case the API returns short pages.
	pageGuard := (cfg.Repos.Max/batch)*2 + 4

	for pageGuard > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		pageGuard--

		// Shrink the page size on the final page so we don't fetch more
		// than cfg.Repos.Max repos overall.
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

	// Pick the most common license; tie-break by first-encountered order.
	if len(licenseOrder) > 0 {
		best := licenseOrder[0]
		bestN := licenseCounts[best]
		for _, spdx := range licenseOrder[1:] {
			if licenseCounts[spdx] > bestN {
				best = spdx
				bestN = licenseCounts[spdx]
			}
		}
		d.Repositories.License = best
	}

	return nil
}

// indepthContribQuery is a minimal contributions-only query used to sum
// activity counts across a date window. The window is bound via the
// $from and $to variables (both DateTime, max 1 year apart).
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

// maxIndepthWindows caps the number of year-long windows we will query when
// summing indepth contributions. GitHub was founded in 2008, so even the
// oldest accounts top out around ~18-19 windows; 20 leaves headroom for
// clock skew and a fresh today vs. CreatedAt edge case.
const maxIndepthWindows = 20

// populateIndepthContributions sums activity counts (commits, PRs opened,
// PRs reviewed, issues opened) across the user's entire history by walking
// 1-year windows from `now` backwards to createdAt. The summed totals
// replace d.Activity.{Commits,PRsOpened,PRsReviewed,IssuesOpened}.
//
// Errors from individual windows abort the loop and the caller leaves the
// last-year values intact (no partial overwrite).
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

		// Once we've covered back to the account's creation, stop.
		if !from.After(createdAt) {
			break
		}

		// Advance the window backwards by 1 year.
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

// maxAuthoringPatterns caps how many commits_authoring entries we will
// search. GitHub's commit-search REST endpoint is rate-limited to 30
// requests/minute, so we keep this conservative to avoid burning the
// whole quota on a single render. Entries beyond the cap are logged
// and skipped.
const maxAuthoringPatterns = 5

// populateAuthoredCommitCount iterates each cfg.CommitsAuthoring entry,
// translates it to a commit-search query (author:<login> for login-style
// entries, author-email:<email> otherwise), and issues a paginated
// search.commits REST call with PerPage=1 to read result.GetTotal().
// The per-pattern totals are summed into d.Activity.AuthoredCommits,
// and on success d.Activity.Commits is REPLACED by that sum (matching
// upstream's behavior when commits_authoring is configured).
//
// The function is intentionally tolerant: errors from individual patterns
// are logged and skipped. If every pattern errors and the running total
// is zero, the function returns the first error so the caller can log a
// single representative failure and leave d.Activity.Commits untouched.
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
		// No patterns produced a query (all empty/blank); leave Commits as-is.
		return nil
	}

	d.Activity.AuthoredCommits = total
	d.Activity.Commits = total
	return nil
}

// buildCommitSearchQuery turns a single commits_authoring entry into a
// GitHub commit-search query string. The placeholder ".user.login" and
// any entry equal to (or @-prefixed equivalent of) the user's login map
// to "author:<login>"; everything else is treated as an email pattern
// and emitted as "author-email:<entry>".
func buildCommitSearchQuery(entry, login, loginLower string) string {
	if entry == "" {
		return ""
	}
	trimmed := strings.TrimPrefix(entry, "@")
	if entry == ".user.login" || strings.EqualFold(trimmed, login) || strings.EqualFold(trimmed, loginLower) {
		if login == "" {
			return ""
		}
		return "author:" + login
	}
	return "author-email:" + entry
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
