package base

import (
	"github.com/twangodev/gmetrics/internal/plugin"
)

type Activity struct {
	Commits      int
	PRsOpened    int
	PRsReviewed  int
	IssuesOpened int
	Comments     int
	// When non-zero, replaces Commits: the per-pattern search counts multiple email identities.
	AuthoredCommits int
}

type Community struct {
	Orgs      int
	Following int
	Sponsors  int
	Stars     int
	Watching  int
}

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

type DayCount struct {
	Date  string
	Count int
}

type Metadata struct {
	GeneratedAt string
	Scope       string
}

type Data struct {
	User          plugin.UserContext
	AvatarB64     string // data: URL; empty when the avatar fetch failed.
	Activity      Activity
	Community     Community
	Repositories  Repositories
	Calendar      []DayCount
	Metadata      Metadata
	Sections      []string
	Hireable      bool
	ContributedTo int
}
