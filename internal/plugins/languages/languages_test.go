package languages

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/require"
	"github.com/twangodev/gmetrics/internal/plugin"
)

func TestRender_BarHasSegments(t *testing.T) {
	t.Parallel()

	data := Data{
		Sections: []string{"most-used"},
		Details:  []string{"percentage"},
		Total:    1000,
		Langs: []Lang{
			{Name: "Go", Color: "#00ADD8", Bytes: 600, Percent: 0.6},
			{Name: "TypeScript", Color: "#3178c6", Bytes: 300, Percent: 0.3},
			{Name: "Python", Color: "#3572A5", Bytes: 100, Percent: 0.1},
		},
	}

	p := &Plugin{}
	frag, err := p.Render(nil, data)
	require.NoError(t, err)
	require.NotEmpty(t, frag.Body)
	require.Equal(t, fragmentWidth, frag.Width)
	require.Positive(t, frag.Height)

	barSegmentRects := strings.Count(frag.Body, "<rect")
	require.GreaterOrEqual(t, barSegmentRects, 3,
		"want at least 3 <rect> elements in body, got %d:\n%s", barSegmentRects, frag.Body)
	legendMarkers := strings.Count(frag.Body, "<circle")
	require.GreaterOrEqual(t, legendMarkers, 3,
		"want at least 3 <circle> legend markers, got %d:\n%s", legendMarkers, frag.Body)

	require.Contains(t, frag.Body, "#00ADD8")
	require.Contains(t, frag.Body, "#3178c6")
	require.Contains(t, frag.Body, "#3572A5")
}

func TestFetch_NonIndepth_AggregatesAndFilters(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	pageCount := 0
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, req *http.Request) {
		require.Equal(t, http.MethodPost, req.Method)
		pageCount++
		w.Header().Set("Content-Type", "application/json")
		singlePageTwoRepos := map[string]any{
			"data": map[string]any{
				"user": map[string]any{
					"repositories": map[string]any{
						"nodes": []any{
							map[string]any{
								"nameWithOwner": "alice/repo1",
								"isFork":        false,
								"languages": map[string]any{
									"edges": []any{
										map[string]any{"size": 1000, "node": map[string]any{"name": "Go", "color": "#00ADD8"}},
										map[string]any{"size": 200, "node": map[string]any{"name": "Markdown", "color": "#083fa1"}},
									},
								},
							},
							map[string]any{
								"nameWithOwner": "alice/repo2",
								"isFork":        false,
								"languages": map[string]any{
									"edges": []any{
										map[string]any{"size": 500, "node": map[string]any{"name": "Go", "color": "#00ADD8"}},
										map[string]any{"size": 300, "node": map[string]any{"name": "TypeScript", "color": "#3178c6"}},
									},
								},
							},
						},
						"pageInfo": map[string]any{
							"endCursor":   "",
							"hasNextPage": false,
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(singlePageTwoRepos)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	gql := githubv4.NewEnterpriseClient(srv.URL+"/graphql", srv.Client())
	env := &plugin.Env{
		Login:   "alice",
		GraphQL: gql,
	}

	cfg := defaultConfig()
	cfg.Ignored = []string{"markdown"}

	p := &Plugin{}
	raw, err := p.Fetch(context.Background(), env, cfg)
	require.NoError(t, err)
	data, ok := raw.(Data)
	require.True(t, ok, "fetch returned %T, want Data", raw)

	require.Equal(t, 1, pageCount, "expected exactly one GraphQL hop")
	require.False(t, data.Indepth)
	require.Len(t, data.Langs, 2)
	require.Equal(t, "Go", data.Langs[0].Name)
	require.Equal(t, 1500, data.Langs[0].Bytes)
	require.Equal(t, "TypeScript", data.Langs[1].Name)
	require.Equal(t, 300, data.Langs[1].Bytes)
	require.Equal(t, 1800, data.Total)
	require.InDelta(t, 1500.0/1800.0, data.Langs[0].Percent, 1e-9)
	require.InDelta(t, 300.0/1800.0, data.Langs[1].Percent, 1e-9)

	// GraphQL-supplied color wins over the defaultColors fallback.
	require.Equal(t, "#00ADD8", data.Langs[0].Color)
	require.Equal(t, "#3178c6", data.Langs[1].Color)
}

func TestFetch_NonIndepth_EnforcesRepoMax(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"user": map[string]any{"repositories": map[string]any{
				"nodes": []any{
					map[string]any{
						"nameWithOwner": "alice/first", "isFork": false,
						"languages": map[string]any{"edges": []any{
							map[string]any{"size": 10, "node": map[string]any{"name": "Go", "color": "#00ADD8"}},
						}},
					},
					map[string]any{
						"nameWithOwner": "alice/second", "isFork": false,
						"languages": map[string]any{"edges": []any{
							map[string]any{"size": 20, "node": map[string]any{"name": "Rust", "color": "#dea584"}},
						}},
					},
				},
				"pageInfo": map[string]any{"endCursor": "", "hasNextPage": false},
			}}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := defaultConfig()
	cfg.RepoMax = 1
	raw, err := (&Plugin{}).Fetch(context.Background(), &plugin.Env{
		Login:   "alice",
		GraphQL: githubv4.NewEnterpriseClient(srv.URL+"/graphql", srv.Client()),
	}, cfg)
	require.NoError(t, err)
	data := raw.(Data)
	require.Equal(t, 10, data.Total)
	require.Len(t, data.Langs, 1)
	require.Equal(t, "Go", data.Langs[0].Name)
}

func TestIsIgnored_CaseInsensitive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		ignored []string
		want    bool
	}{
		{"Markdown", []string{"markdown"}, true},
		{"markdown", []string{"Markdown"}, true},
		{"MARKDOWN", []string{"markdown"}, true},
		{"Go", []string{"markdown"}, false},
		{"Markdown", nil, false},
		{"Markdown", []string{}, false},
		{"C++", []string{"c++"}, true},
		{"Vim Script", []string{"vim script"}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"|"+strings.Join(tc.ignored, ","), func(t *testing.T) {
			t.Parallel()
			got := isIgnored(tc.name, tc.ignored)
			require.Equal(t, tc.want, got, "isIgnored(%q, %v)", tc.name, tc.ignored)
		})
	}
}
