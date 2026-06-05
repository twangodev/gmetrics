package wakatime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twangodev/gmetrics/internal/plugin"
)

const cannedAPIResponse = `{
  "data": {
    "total_seconds": 12345.67,
    "daily_average": 1234.56,
    "projects": [
      {"name":"foo","percent":42.0,"total_seconds":5000},
      {"name":"bar","percent":30.0,"total_seconds":3700},
      {"name":"baz","percent":28.0,"total_seconds":3645.67}
    ],
    "languages": [
      {"name":"Go","percent":60.0,"total_seconds":7000},
      {"name":"Python","percent":40.0,"total_seconds":5345.67}
    ],
    "editors": [
      {"name":"GoLand","percent":80.0,"total_seconds":9876.5},
      {"name":"Vim","percent":20.0,"total_seconds":2469.17}
    ],
    "operating_systems": [
      {"name":"Linux","percent":100.0,"total_seconds":12345.67}
    ]
  }
}`

func TestFetch_DecodesAPIResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/api/v1/users/current/stats/last_7_days")
		require.Equal(t, "abc123", r.URL.Query().Get("api_key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cannedAPIResponse))
	}))
	t.Cleanup(srv.Close)

	env := &plugin.Env{HTTP: srv.Client()}
	cfg := defaultConfig()
	cfg.URL = srv.URL
	cfg.Token = "abc123"

	data, err := fetch(context.Background(), env, cfg)
	require.NoError(t, err)
	require.InDelta(t, 3.43, data.TotalHours, 0.01)
	require.InDelta(t, 1234.56/3600.0, data.DailyAvgHours, 0.001)
	require.NotEmpty(t, data.Projects)
	require.NotEmpty(t, data.Languages)
	require.NotEmpty(t, data.Editors)
	require.NotEmpty(t, data.OSes)
	// API reports percent in 0-100; fetch normalizes to 0-1.
	require.InDelta(t, 0.60, data.Languages[0].Percent, 0.001)
	require.Equal(t, "foo", data.Projects[0].Name, "highest percent sorts first")
}

func TestRender_AllSections_ProducesSVG(t *testing.T) {
	data := Data{
		Sections: []string{
			"time",
			"projects", "projects-graphs",
			"languages", "languages-graphs",
			"editors", "editors-graphs",
			"os", "os-graphs",
		},
		Days:          7,
		Limit:         5,
		TotalHours:    3.43,
		DailyAvgHours: 0.343,
		Projects: []Item{
			{Name: "foo", Percent: 0.42, Seconds: 5000},
			{Name: "bar", Percent: 0.30, Seconds: 3700},
			{Name: "baz", Percent: 0.28, Seconds: 3645},
		},
		Languages: []Item{
			{Name: "Go", Percent: 0.60, Seconds: 7000},
			{Name: "Python", Percent: 0.40, Seconds: 5345},
		},
		Editors: []Item{
			{Name: "GoLand", Percent: 0.80, Seconds: 9876},
			{Name: "Vim", Percent: 0.20, Seconds: 2469},
		},
		OSes: []Item{
			{Name: "Linux", Percent: 1.00, Seconds: 12345},
		},
	}

	frag, err := renderFragment(nil, data)
	require.NoError(t, err)
	require.NotEmpty(t, frag.Body)
	require.Greater(t, frag.Height, 100)
	require.Equal(t, 440, frag.Width)
	// Framer wraps the fragment at compose time; an outer SVG document here is a regression (inline octicon `<svg x=...>` glyphs are fine).
	require.NotContains(t, frag.Body, `<svg xmlns`,
		"fragment must not contain an outer SVG document element")
	require.NotContains(t, frag.Body, `<svg width=`,
		"fragment must not contain an outer SVG document element")
}

func TestPlugin_RegistersUnderName(t *testing.T) {
	p, ok := plugin.Lookup("wakatime")
	require.True(t, ok)
	require.Equal(t, "wakatime", p.Name())
}
