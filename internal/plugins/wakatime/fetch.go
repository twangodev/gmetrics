package wakatime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"

	"github.com/twangodev/gmetrics/internal/plugin"
)

const (
	secondsPerHour    = 3600
	percentToFraction = 100.0
)

type apiResponse struct {
	Data struct {
		TotalSeconds float64       `json:"total_seconds"`
		DailyAverage float64       `json:"daily_average"`
		Projects     []rawCategory `json:"projects"`
		Languages    []rawCategory `json:"languages"`
		Editors      []rawCategory `json:"editors"`
		OperatingSys []rawCategory `json:"operating_systems"`
	} `json:"data"`
}

type rawCategory struct {
	Name         string  `json:"name"`
	Percent      float64 `json:"percent"`
	TotalSeconds float64 `json:"total_seconds"`
}

func fetch(ctx context.Context, env *plugin.Env, cfg Config) (Data, error) {
	cfg = applyConfigDefaults(cfg)
	endpoint := fmt.Sprintf(
		"%s/api/v1/users/%s/stats/%s?api_key=%s",
		cfg.URL,
		url.PathEscape(cfg.User),
		rangeForDays(cfg.Days),
		url.QueryEscape(cfg.Token),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Data{}, fmt.Errorf("wakatime: build request: %w", err)
	}

	client := http.DefaultClient
	if env != nil && env.HTTP != nil {
		client = env.HTTP
	}

	resp, err := client.Do(req)
	if err != nil {
		return Data{}, fmt.Errorf("wakatime: get stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Data{}, fmt.Errorf("wakatime: status %d: %s", resp.StatusCode, string(body))
	}

	var parsed apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return Data{}, fmt.Errorf("wakatime: decode response: %w", err)
	}

	d := Data{
		Sections:      append([]string(nil), cfg.Sections...),
		Days:          cfg.Days,
		Limit:         cfg.Limit,
		TotalHours:    parsed.Data.TotalSeconds / secondsPerHour,
		DailyAvgHours: parsed.Data.DailyAverage / secondsPerHour,
		Projects:      shapeCategory(parsed.Data.Projects, cfg.Limit),
		Languages:     shapeCategory(parsed.Data.Languages, cfg.Limit),
		Editors:       shapeCategory(parsed.Data.Editors, cfg.Limit),
		OSes:          shapeCategory(parsed.Data.OperatingSys, cfg.Limit),
	}
	return d, nil
}

func applyConfigDefaults(c Config) Config {
	if c.URL == "" {
		c.URL = "https://wakatime.com"
	}
	if c.User == "" {
		c.User = "current"
	}
	if c.Days == 0 {
		c.Days = 7
	}
	if c.Limit == 0 {
		c.Limit = 5
	}
	if len(c.Sections) == 0 {
		c.Sections = []string{"time", "projects", "languages", "editors", "os"}
	}
	return c
}

func shapeCategory(in []rawCategory, limit int) []Item {
	if len(in) == 0 {
		return nil
	}
	out := make([]Item, 0, len(in))
	for _, r := range in {
		out = append(out, Item{
			Name:    r.Name,
			Percent: r.Percent / percentToFraction,
			Seconds: r.TotalSeconds,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Percent > out[j].Percent
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
