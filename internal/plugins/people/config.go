package people

import (
	"fmt"
	"math"
)

type Config struct {
	Types []string `koanf:"types"`
	// Limit is a single GraphQL page; GitHub caps first at 100.
	Limit int `koanf:"limit"`
	// Size is the avatar pixel size; the avatar URL is fetched at 2x for retina.
	Size       int     `koanf:"size"`
	MaxOverlap float64 `koanf:"max_overlap"`
}

func defaultConfig() Config {
	return Config{
		Types:      []string{"followers", "following"},
		Limit:      40,
		Size:       28,
		MaxOverlap: 0.4,
	}
}

func (c Config) validate() error {
	if c.Limit <= 0 {
		return fmt.Errorf("people: limit must be > 0")
	}
	if err := validateLayout(c.Size, c.MaxOverlap); err != nil {
		return err
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

func validateLayout(size int, maxOverlap float64) error {
	if size <= 0 || size > fragmentWidth {
		return fmt.Errorf("people: size must be between 1 and %d", fragmentWidth)
	}
	if math.IsNaN(maxOverlap) || maxOverlap < 0 || maxOverlap >= 1 {
		return fmt.Errorf("people: max_overlap must be >= 0 and < 1")
	}
	return nil
}
