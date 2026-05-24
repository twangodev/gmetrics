package people_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
	"github.com/stretchr/testify/require"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/plugins/people"
)

// TestRender_TwoSections verifies the render path produces a well-formed
// Fragment with the expected dimensions for two sections (one with 3
// people, one with 2). Avatars are deliberately left empty (placeholder
// circles) so this test does not touch the network.
func TestRender_TwoSections(t *testing.T) {
	p := &people.Plugin{}
	data := people.Data{
		Size: 28,
		Sections: []people.Section{
			{
				Type:  "followers",
				Total: 1234,
				People: []people.Person{
					{Login: "alice"},
					{Login: "bob"},
					{Login: "carol"},
				},
			},
			{
				Type:  "following",
				Total: 42,
				People: []people.Person{
					{Login: "dave"},
					{Login: "eve"},
				},
			},
		},
	}

	frag, err := p.Render(nil, data)
	require.NoError(t, err)
	require.Equal(t, 440, frag.Width)
	require.GreaterOrEqual(t, frag.Height, 84,
		"two sections with header + one row each should be at least 2 * (28 + 28 + 4 + 8) ~= 136 px")

	// Sanity: the body must contain both section markers and a placeholder
	// circle for every person (5 people total). Section headers are
	// rendered as glyph <path> elements (text-as-path) so we can't grep
	// for the literal "Followers (3)" substring; instead we assert one
	// header <path> per section.
	require.Contains(t, frag.Body, `data-type="followers"`)
	require.Contains(t, frag.Body, `data-type="following"`)
	require.Equal(t, 5, strings.Count(frag.Body, "<circle"))
	require.GreaterOrEqual(t, strings.Count(frag.Body, "<path"), 2,
		"each section should contribute one header <path> element")
}

// TestFetch_FollowersAndFollowing_Counts spins up a mock GraphQL endpoint
// that distinguishes followers vs following by inspecting the query body
// and returns a canned set of nodes. env.HTTP is intentionally nil so the
// plugin skips the avatar fetch step; we only assert that the structure
// and counts come through correctly.
func TestFetch_FollowersAndFollowing_Counts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(s, "followers("):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"user": map[string]any{
						"followers": map[string]any{
							"totalCount": 1234,
							"nodes": []map[string]any{
								{"login": "alice", "avatarUrl": "https://example.invalid/a.png"},
								{"login": "bob", "avatarUrl": "https://example.invalid/b.png"},
							},
						},
					},
				},
			})
		case strings.Contains(s, "following("):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"user": map[string]any{
						"following": map[string]any{
							"totalCount": 42,
							"nodes": []map[string]any{
								{"login": "carol", "avatarUrl": "https://example.invalid/c.png"},
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected query body: %s", s)
		}
	}))
	defer srv.Close()

	gql := githubv4.NewEnterpriseClient(srv.URL, srv.Client())
	env := &plugin.Env{
		Login:   "twangodev",
		GraphQL: gql,
		HTTP:    nil, // skip avatar fetch
	}

	p := &people.Plugin{}
	raw, err := p.DecodeConfig(map[string]any{
		"types": []any{"followers", "following"},
		"limit": 24,
		"size":  28,
	})
	require.NoError(t, err)

	out, err := p.Fetch(context.Background(), env, raw)
	require.NoError(t, err)

	data, ok := out.(people.Data)
	require.True(t, ok, "Fetch must return people.Data, got %T", out)
	require.Len(t, data.Sections, 2)
	require.Equal(t, "followers", data.Sections[0].Type)
	require.Equal(t, 1234, data.Sections[0].Total,
		"Section.Total should be populated from user.followers.totalCount, "+
			"not derived from the number of returned nodes")
	require.Len(t, data.Sections[0].People, 2)
	require.Equal(t, "alice", data.Sections[0].People[0].Login)
	require.Empty(t, data.Sections[0].People[0].AvatarB64,
		"avatars should be unfetched when env.HTTP is nil")
	require.Equal(t, "following", data.Sections[1].Type)
	require.Equal(t, 42, data.Sections[1].Total,
		"Section.Total should be populated from user.following.totalCount")
	require.Len(t, data.Sections[1].People, 1)
	require.Equal(t, "carol", data.Sections[1].People[0].Login)
}
