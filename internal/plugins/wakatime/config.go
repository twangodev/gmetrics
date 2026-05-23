package wakatime

import "fmt"

// Config is the typed configuration for the wakatime plugin.
type Config struct {
	// Token is the WakaTime API key (https://wakatime.com/api-key).
	Token string
	// URL is the WakaTime base URL. Defaults to "https://wakatime.com".
	URL string
	// User is the WakaTime user identifier. Defaults to "current", which the
	// API resolves to the token's owner.
	User string
	// Days controls the lookback range. Mapped to API ranges:
	//   7 -> last_7_days
	//   30 -> last_30_days
	//   180 -> last_6_months
	//   365 -> last_year
	// Other values fall back to last_7_days.
	Days int
	// Sections is the ordered list of sections to render. Recognized values
	// include "time", "projects", "languages", "editors", "os" and the same
	// names with a "-graphs" suffix for the bar-chart variants.
	Sections []string
	// Limit is the maximum number of rows per graph/category. Defaults to 5.
	Limit int
}

// defaultConfig returns the baseline configuration used when none is supplied
// or as the starting point for decoding from YAML.
func defaultConfig() Config {
	return Config{
		URL:      "https://wakatime.com",
		User:     "current",
		Days:     7,
		Limit:    5,
		Sections: []string{"time", "projects", "languages", "editors", "os"},
	}
}

// decodeConfig converts a raw YAML map into a Config, filling defaults for
// any missing fields. Returning an empty/zero raw map yields the defaults.
func decodeConfig(raw map[string]any) (Config, error) {
	c := defaultConfig()
	if raw == nil {
		return c, nil
	}
	if v, ok := raw["token"]; ok {
		s, ok := v.(string)
		if !ok {
			return c, fmt.Errorf("wakatime: token must be a string")
		}
		c.Token = s
	}
	if v, ok := raw["url"]; ok {
		s, ok := v.(string)
		if !ok {
			return c, fmt.Errorf("wakatime: url must be a string")
		}
		if s != "" {
			c.URL = s
		}
	}
	if v, ok := raw["user"]; ok {
		s, ok := v.(string)
		if !ok {
			return c, fmt.Errorf("wakatime: user must be a string")
		}
		if s != "" {
			c.User = s
		}
	}
	if v, ok := raw["days"]; ok {
		switch n := v.(type) {
		case int:
			if n > 0 {
				c.Days = n
			}
		case int64:
			if n > 0 {
				c.Days = int(n)
			}
		case float64:
			if n > 0 {
				c.Days = int(n)
			}
		default:
			return c, fmt.Errorf("wakatime: days must be a number")
		}
	}
	if v, ok := raw["limit"]; ok {
		switch n := v.(type) {
		case int:
			if n > 0 {
				c.Limit = n
			}
		case int64:
			if n > 0 {
				c.Limit = int(n)
			}
		case float64:
			if n > 0 {
				c.Limit = int(n)
			}
		default:
			return c, fmt.Errorf("wakatime: limit must be a number")
		}
	}
	if v, ok := raw["sections"]; ok {
		switch arr := v.(type) {
		case []any:
			out := make([]string, 0, len(arr))
			for _, item := range arr {
				s, ok := item.(string)
				if !ok {
					return c, fmt.Errorf("wakatime: sections entries must be strings")
				}
				out = append(out, s)
			}
			c.Sections = out
		case []string:
			c.Sections = append([]string(nil), arr...)
		default:
			return c, fmt.Errorf("wakatime: sections must be a list")
		}
	}
	return c, nil
}

// rangeForDays maps a Days value to the WakaTime API range slug. Unknown
// values fall back to "last_7_days" to match upstream's behavior.
func rangeForDays(days int) string {
	switch days {
	case 7:
		return "last_7_days"
	case 30:
		return "last_30_days"
	case 180:
		return "last_6_months"
	case 365:
		return "last_year"
	default:
		return "last_7_days"
	}
}
