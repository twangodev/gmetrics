package base

import (
	"github.com/twangodev/gmetrics/internal/plugin"
)

// Activity holds the contribution-collection counts surfaced in the
// "activity" section of the card.
type Activity struct {
	Commits      int
	PRsOpened    int
	PRsReviewed  int
	IssuesOpened int
	Comments     int
	// AuthoredCommits is the sum of commit-search totals across the
	// configured commits_authoring patterns. Populated only when
	// cfg.CommitsAuthoring is non-empty and the REST search succeeded.
	// When populated, it replaces Commits (the per-pattern search is the
	// more accurate count for users with multiple email identities).
	AuthoredCommits int
}

// Community holds the social-graph counts surfaced in the "community"
// section of the card.
type Community struct {
	Orgs      int
	Following int
	Sponsors  int
	Stars     int
	Watching  int
}

// Repositories holds the aggregate counts surfaced in the "repositories"
// section of the card.
type Repositories struct {
	Count      int
	Disk       int
	Releases   int
	Packages   int
	Forks      int
	Watchers   int
	Stargazers int
	License    string
}

// DayCount is a single bucket in the contribution calendar mini-chart.
type DayCount struct {
	Date  string
	Count int
}

// Metadata is rendered as a tiny footer line below the other sections.
type Metadata struct {
	GeneratedAt string
	Scope       string
}

// Data is the value Fetch returns and Render consumes.
type Data struct {
	User          plugin.UserContext
	AvatarB64     string // data: URL for the user's avatar; empty if fetch failed or env.HTTP was nil
	Activity      Activity
	Community     Community
	Repositories  Repositories
	Calendar      []DayCount
	Metadata      Metadata
	Sections      []string
	Hireable      bool
	ContributedTo int // count of external repositories the user has contributed to (PRs/issues/commits/repos)
}
