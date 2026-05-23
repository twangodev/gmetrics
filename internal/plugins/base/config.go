package base

// Config is the typed configuration for the base plugin. It mirrors the
// `base.*` block in YAML (see internal/config.BaseConfig) but lives in this
// package so the plugin can keep its types self-contained.
type Config struct {
	// Sections is the ordered list of sub-sections to render. Each value
	// must be one of "header", "activity", "community", "repositories",
	// "metadata".
	Sections []string `koanf:"sections"`
	// Hireable, when true, surfaces an "Available for hire" badge in the
	// header section. Mirrors upstream's base_hireable input.
	Hireable bool `koanf:"hireable"`
	// Indepth, when true, signals that activity stats should be fetched
	// across the user's entire history rather than the default rolling
	// window. v1 stores the flag but does not implement the multi-window
	// query (stretch goal); upstream behavior is approximated.
	Indepth bool `koanf:"indepth"`
	// CommitsAuthoring is the list of authoring email addresses upstream
	// uses for commit-count search. v1 stores but does not consume it.
	CommitsAuthoring []string `koanf:"commits_authoring"`
	// Repos controls how the repositories collection is queried.
	Repos RepoFetch `koanf:"repositories"`
}

// RepoFetch controls how repositories are queried for the base plugin.
type RepoFetch struct {
	// Affiliations is the list of ownership affiliations to include. Each
	// value is one of "owner", "collaborator", "organization_member".
	Affiliations []string `koanf:"affiliations"`
	// Max caps the total number of repositories considered (v1 metadata).
	Max int `koanf:"max"`
	// Batch is the GraphQL page size to use (v1 metadata).
	Batch int `koanf:"batch"`
	// Forks, when true, includes fork repositories in the collection.
	Forks bool `koanf:"forks"`
}

// defaultConfig returns the documented defaults for the base plugin so
// callers that construct a Plugin directly (e.g. tests, the engine when no
// config block was supplied) get sensible behavior.
func defaultConfig() Config {
	return Config{
		Sections: []string{"header", "activity", "community", "repositories", "metadata"},
		Hireable: false,
		Indepth:  false,
		Repos: RepoFetch{
			Affiliations: []string{"owner"},
			Max:          100,
			Batch:        100,
			Forks:        false,
		},
	}
}
