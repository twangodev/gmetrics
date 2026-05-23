package people

import (
	"fmt"
)

// Config is the typed configuration for the people plugin. It mirrors the
// in-scope subset of upstream's people plugin: only the followers/following
// types are supported in v1.
type Config struct {
	// Types is the ordered list of people sections to render. Each value
	// must be one of "followers", "following".
	Types []string `koanf:"types"`
	// Limit caps the number of people fetched per type (GitHub allows up
	// to 100 per page; we always issue a single page).
	Limit int `koanf:"limit"`
	// Size is the avatar render size in pixels. The avatar URL is fetched
	// at 2x this size to look crisp on retina displays.
	Size int `koanf:"size"`
}

// defaultConfig returns a Config with the user's documented defaults applied
// (Types: followers + following, Limit: 24, Size: 28).
func defaultConfig() Config {
	return Config{
		Types: []string{"followers", "following"},
		Limit: 24,
		Size:  28,
	}
}

// validate enforces the v1 invariants on a Config. It returns an error
// describing the first issue found, suitable for surfacing to the user.
func (c Config) validate() error {
	if c.Limit <= 0 {
		return fmt.Errorf("people: limit must be > 0")
	}
	if c.Size <= 0 {
		return fmt.Errorf("people: size must be > 0")
	}
	if len(c.Types) == 0 {
		return fmt.Errorf("people: types must not be empty")
	}
	for _, t := range c.Types {
		switch t {
		case "followers", "following":
			// ok
		default:
			return fmt.Errorf("people: unsupported type %q (v1 supports followers, following)", t)
		}
	}
	return nil
}
