package people_test

import (
	"context"
	"encoding/json"
	"fmt"
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
	require.GreaterOrEqual(t, frag.Height, 84)

	require.Contains(t, frag.Body, `data-type="followers"`)
	require.Contains(t, frag.Body, `data-type="following"`)

	const totalPeopleAndOverflowMarkers = 7
	require.Equal(t, totalPeopleAndOverflowMarkers, strings.Count(frag.Body, "<circle"))
	require.Contains(t, frag.Body, `data-overflow="1231"`)
	require.Contains(t, frag.Body, `data-overflow="40"`)

	// Section headers are rendered as text-as-path glyphs, so assert one header <path> per section rather than grepping header text.
	const sectionCount = 2
	require.GreaterOrEqual(t, strings.Count(frag.Body, "<path"), sectionCount)
}

func TestRender_SquashesFortyPeopleIntoTwoRows(t *testing.T) {
	peopleList := make([]people.Person, 40)
	for i := range peopleList {
		peopleList[i] = people.Person{Login: fmt.Sprintf("person-%02d", i)}
	}

	frag, err := (&people.Plugin{}).Render(nil, people.Data{
		Size: 28,
		Sections: []people.Section{{
			Type:   "followers",
			Total:  40,
			People: peopleList,
		}},
	})
	require.NoError(t, err)
	require.Equal(t, 80, frag.Height)
	require.Equal(t, 40, strings.Count(frag.Body, `<circle`))
	require.Equal(t, 40, strings.Count(frag.Body, `r="9"`))
	require.NotContains(t, frag.Body, `people-overflow`)
}

func TestRender_UsesFinalSlotForOverflow(t *testing.T) {
	peopleList := make([]people.Person, 40)
	for i := range peopleList {
		peopleList[i] = people.Person{Login: fmt.Sprintf("person-%02d", i)}
	}

	frag, err := (&people.Plugin{}).Render(nil, people.Data{
		Size: 28,
		Sections: []people.Section{{
			Type:   "followers",
			Total:  50,
			People: peopleList,
		}},
	})
	require.NoError(t, err)
	require.Equal(t, 80, frag.Height)
	require.Equal(t, 40, strings.Count(frag.Body, `<circle`))
	require.Contains(t, frag.Body, `data-overflow="11"`)
	require.Contains(t, frag.Body, `<title>11 more</title>`)
	require.Contains(t, frag.Body, `<title>person-38</title>`)
	require.NotContains(t, frag.Body, `<title>person-39</title>`)
}

func TestFetch_FollowersAndFollowing_Counts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		query := string(body)

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(query, "followers("):
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
		case strings.Contains(query, "following("):
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
			t.Fatalf("unexpected query body: %s", query)
		}
	}))
	defer srv.Close()

	gql := githubv4.NewEnterpriseClient(srv.URL, srv.Client())
	env := &plugin.Env{
		Login:   "twangodev",
		GraphQL: gql,
		HTTP:    nil,
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
		"Total comes from totalCount, not the returned node count")
	require.Len(t, data.Sections[0].People, 2)
	require.Equal(t, "alice", data.Sections[0].People[0].Login)
	require.Empty(t, data.Sections[0].People[0].AvatarB64,
		"avatars should be unfetched when env.HTTP is nil")
	require.Equal(t, "following", data.Sections[1].Type)
	require.Equal(t, 42, data.Sections[1].Total,
		"Total comes from totalCount, not the returned node count")
	require.Len(t, data.Sections[1].People, 1)
	require.Equal(t, "carol", data.Sections[1].People[0].Login)
}
