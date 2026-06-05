package people

import (
	"fmt"
)

type Config struct {
	Types []string `koanf:"types"`
	// Limit is a single GraphQL page; GitHub caps first at 100.
	Limit int `koanf:"limit"`
	// Size is the avatar pixel size; the avatar URL is fetched at 2x for retina.
	Size int `koanf:"size"`
}

func defaultConfig() Config {
	return Config{
		Types: []string{"followers", "following"},
		Limit: 24,
		Size:  28,
	}
}

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
		default:
			return fmt.Errorf("people: unsupported type %q (v1 supports followers, following)", t)
		}
	}
	return nil
}
