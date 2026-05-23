package languages

import "fmt"

// Config is the typed configuration for the languages plugin. It mirrors the
// in-scope subset of upstream's languages plugin: sections is single-valued
// at "most-used" for v1, details is at most a single entry "percentage", and
// the optional Indepth flag toggles between the cheap GraphQL aggregate path
// and the expensive go-git + go-enry walk.
type Config struct {
	// Sections is the ordered list of subsections to render. v1 supports
	// only "most-used"; "recently-used" is intentionally out of scope.
	Sections []string `koanf:"sections"`
	// Details is the ordered list of per-language detail fields shown in
	// the legend. v1 supports only "percentage".
	Details []string `koanf:"details"`
	// Ignored is a case-insensitive list of language names to drop from the
	// aggregate before sorting (e.g. ["markdown"]).
	Ignored []string `koanf:"ignored"`
	// Limit caps the number of legend rows. Excess languages are either
	// dropped (Other=false) or rolled up into an "Other" bucket.
	Limit int `koanf:"limit"`
	// Other, when true, rolls excess languages into an "Other" bucket at
	// the end of the legend.
	Other bool `koanf:"other"`
	// Indepth, when true, switches the fetch path from the GraphQL
	// aggregate (GitHub's pre-computed totals) to per-repo shallow clone +
	// go-enry walk. Expensive: minutes of runtime and bandwidth scaling
	// with repo count.
	Indepth bool `koanf:"indepth"`
	// RepoBatch is the per-page GraphQL page size when walking the user's
	// repositories. Defaults to 50.
	RepoBatch int `koanf:"repo_batch"`
	// RepoAffiliations is the list of ownership affiliations to include
	// (upstream's base.repositories.affiliations). Each value is one of
	// "owner", "collaborator", "organization_member". Defaults to
	// ["owner"].
	RepoAffiliations []string `koanf:"repo_affiliations"`
}

// defaultConfig returns the documented defaults for the languages plugin so
// callers that construct a Plugin directly (e.g. tests, the engine when no
// config block was supplied) get sensible behavior.
func defaultConfig() Config {
	return Config{
		Sections:         []string{"most-used"},
		Details:          []string{"percentage"},
		Ignored:          nil,
		Limit:            8,
		Other:            false,
		Indepth:          false,
		RepoBatch:        50,
		RepoAffiliations: []string{"owner"},
	}
}

// validate enforces the v1 invariants on a Config. It returns an error
// describing the first issue found, suitable for surfacing to the user.
func (c Config) validate() error {
	if c.Limit <= 0 {
		return fmt.Errorf("languages: limit must be > 0")
	}
	if c.RepoBatch <= 0 {
		return fmt.Errorf("languages: repo_batch must be > 0")
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
