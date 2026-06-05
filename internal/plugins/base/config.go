package base

type Config struct {
	Sections []string `koanf:"sections"`
	// Badge follows GitHub's live "Available for hire" status; it never force-shows.
	Hireable bool `koanf:"hireable"`
	// Indepth and CommitsAuthoring are stored but not yet consumed in v1.
	Indepth          bool      `koanf:"indepth"`
	CommitsAuthoring []string  `koanf:"commits_authoring"`
	Repos            RepoFetch `koanf:"repositories"`
}

type RepoFetch struct {
	Affiliations []string `koanf:"affiliations"`
	Max          int      `koanf:"max"`
	Batch        int      `koanf:"batch"`
	Forks        bool     `koanf:"forks"`
}

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
