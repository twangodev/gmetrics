package languages

import "fmt"

type Config struct {
	Sections         []string `koanf:"sections"`
	Details          []string `koanf:"details"`
	Ignored          []string `koanf:"ignored"`
	Limit            int      `koanf:"limit"`
	Other            bool     `koanf:"other"`
	Indepth          bool     `koanf:"indepth"`
	RepoBatch        int      `koanf:"repo_batch"`
	RepoMax          int      `koanf:"repo_max"`
	RepoAffiliations []string `koanf:"repo_affiliations"`
	// CommitsAuthoring is consulted only by the indepth path; the GraphQL path relies on GitHub's per-repo aggregates.
	CommitsAuthoring []string `koanf:"commits_authoring"`
	IndepthCachePath string   `koanf:"indepth_cache"`
}

func defaultConfig() Config {
	return Config{
		Sections:         []string{"most-used"},
		Details:          []string{"percentage"},
		Ignored:          nil,
		Limit:            8,
		Other:            false,
		Indepth:          false,
		RepoBatch:        50,
		RepoMax:          100,
		RepoAffiliations: []string{"owner"},
	}
}

func (c Config) validate() error {
	if c.Limit <= 0 {
		return fmt.Errorf("languages: limit must be > 0")
	}
	if c.RepoBatch <= 0 {
		return fmt.Errorf("languages: repo_batch must be > 0")
	}
	if c.RepoMax < 0 {
		return fmt.Errorf("languages: repo_max must be >= 0")
	}
	for _, affiliation := range c.RepoAffiliations {
		switch affiliation {
		case "owner", "collaborator", "organization_member":
		default:
			return fmt.Errorf("languages: unsupported repository affiliation %q", affiliation)
		}
	}
	if len(c.Sections) == 0 {
		return fmt.Errorf("languages: sections must not be empty")
	}
	for _, s := range c.Sections {
		if s != "most-used" {
			return fmt.Errorf("languages: unsupported section %q (v1 supports most-used)", s)
		}
	}
	for _, d := range c.Details {
		if d != "percentage" {
			return fmt.Errorf("languages: unsupported detail %q (v1 supports percentage)", d)
		}
	}
	return nil
}
