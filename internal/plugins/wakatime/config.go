package wakatime

import "fmt"

type Config struct {
	Token    string
	URL      string
	User     string
	Days     int
	Sections []string
	Limit    int
}

func defaultConfig() Config {
	return Config{
		URL:   "https://wakatime.com",
		User:  "current", // WakaTime resolves "current" to the token's owner.
		Days:  7,
		Limit: 5,
		Sections: []string{
			"time", "projects", "languages", "editors", "os",
			"projects-graphs", "languages-graphs", "editors-graphs", "os-graphs",
		},
	}
}

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
