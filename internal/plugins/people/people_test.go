package people_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-github/v66/github"
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

func TestRender_OverlapsFortyPeopleWithoutShrinking(t *testing.T) {
	peopleList := make([]people.Person, 40)
	for i := range peopleList {
		peopleList[i] = people.Person{Login: fmt.Sprintf("person-%02d", i)}
	}

	frag, err := (&people.Plugin{}).Render(nil, people.Data{
		Size:       28,
		MaxOverlap: 0.4,
		Sections: []people.Section{{
			Type:   "followers",
			Total:  40,
			People: peopleList,
		}},
	})
	require.NoError(t, err)
	require.Equal(t, 100, frag.Height)
	require.Equal(t, 40, strings.Count(frag.Body, `data-account-type="user"`))
	require.Equal(t, 40, strings.Count(frag.Body, `r="14"`))
	require.NotContains(t, frag.Body, `people-overflow`)
}

func TestRender_OverflowDoesNotReplaceFetchedPeople(t *testing.T) {
	peopleList := make([]people.Person, 40)
	for i := range peopleList {
		peopleList[i] = people.Person{Login: fmt.Sprintf("person-%02d", i)}
	}

	frag, err := (&people.Plugin{}).Render(nil, people.Data{
		Size:       28,
		MaxOverlap: 0.4,
		Sections: []people.Section{{
			Type:   "followers",
			Total:  50,
			People: peopleList,
		}},
	})
	require.NoError(t, err)
	require.Equal(t, 100, frag.Height)
	require.Equal(t, 40, strings.Count(frag.Body, `data-account-type="user"`))
	require.Contains(t, frag.Body, `data-overflow="10"`)
	require.Contains(t, frag.Body, `<title>10 more</title>`)
	require.Contains(t, frag.Body, `<title>person-39</title>`)
}

func TestRender_OrganizationsUseRoundedSquareAvatars(t *testing.T) {
	frag, err := (&people.Plugin{}).Render(nil, people.Data{
		Size: 28,
		Sections: []people.Section{{
			Type:  "following",
			Total: 2,
			People: []people.Person{
				{Login: "acme", IsOrganization: true, AvatarB64: "data:image/png;base64,AA=="},
				{Login: "alice", AvatarB64: "data:image/png;base64,AA=="},
			},
		}},
	})
	require.NoError(t, err)
	require.Contains(t, frag.Body, `data-account-type="organization"`)
	require.Regexp(t, `<clipPath id="avatar-clip-acme-[^"]+"><rect x="[0-9]+" y="28" width="28" height="28" rx="4" ry="4"/></clipPath>`, frag.Body)
	require.Contains(t, frag.Body, `data-account-type="user"`)
	require.Regexp(t, `<clipPath id="avatar-clip-alice-[^"]+"><circle cx="[0-9]+" cy="42" r="14"/></clipPath>`, frag.Body)
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
	require.Equal(t, 0.4, data.MaxOverlap)
	require.Empty(t, data.Sections[0].People[0].AvatarB64,
		"avatars should be unfetched when env.HTTP is nil")
	require.Equal(t, "following", data.Sections[1].Type)
	require.Equal(t, 42, data.Sections[1].Total,
		"Total comes from totalCount, not the returned node count")
	require.Len(t, data.Sections[1].People, 1)
	require.Equal(t, "carol", data.Sections[1].People[0].Login)
}

func TestFetch_RESTIncludesOrganizations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/twangodev":
			_, _ = io.WriteString(w, `{"login":"twangodev","followers":1,"following":2}`)
		case "/users/twangodev/followers":
			_, _ = io.WriteString(w, `[{"login":"alice","type":"User","avatar_url":"https://example.invalid/alice.png"}]`)
		case "/users/twangodev/following":
			_, _ = io.WriteString(w, `[{"login":"acme","type":"Organization","avatar_url":"https://example.invalid/acme.png"},{"login":"bob","type":"User","avatar_url":"https://example.invalid/bob.png"}]`)
		default:
			t.Fatalf("unexpected REST path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	baseURL, err := url.Parse(srv.URL + "/")
	require.NoError(t, err)
	rest := github.NewClient(srv.Client())
	rest.BaseURL = baseURL

	raw, err := (&people.Plugin{}).DecodeConfig(map[string]any{
		"types":       []any{"followers", "following"},
		"limit":       40,
		"size":        28,
		"max_overlap": 0.25,
	})
	require.NoError(t, err)
	out, err := (&people.Plugin{}).Fetch(context.Background(), &plugin.Env{
		Login: "twangodev",
		REST:  rest,
	}, raw)
	require.NoError(t, err)

	data := out.(people.Data)
	require.Equal(t, 0.25, data.MaxOverlap)
	require.Equal(t, 1, data.Sections[0].Total)
	require.False(t, data.Sections[0].People[0].IsOrganization)
	require.Equal(t, 2, data.Sections[1].Total)
	require.True(t, data.Sections[1].People[0].IsOrganization)
	require.Equal(t, "acme", data.Sections[1].People[0].Login)
	require.False(t, data.Sections[1].People[1].IsOrganization)
}

type renderedAvatar struct {
	X      int    `xml:"x,attr"`
	Y      int    `xml:"y,attr"`
	Width  int    `xml:"width,attr"`
	Height int    `xml:"height,attr"`
	Title  string `xml:"title"`
}

type renderedSection struct {
	Type    string           `xml:"data-type,attr"`
	Avatars []renderedAvatar `xml:"image"`
}

func readRenderedSections(t *testing.T, body string) []renderedSection {
	t.Helper()
	var svg struct {
		Sections []renderedSection `xml:"g"`
	}
	require.NoError(t, xml.Unmarshal([]byte("<svg>"+body+"</svg>"), &svg))
	return svg.Sections
}

func TestRender_RowWrapping(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		count, size, step, lastStep, cols int
		overlap                           float64
	}{
		{"empty", 0, 28, 32, 32, 1, 0.4},
		{"single", 1, 28, 32, 32, 1, 0.4},
		{"short row", 12, 28, 32, 32, 12, 0.4},
		{"normal row", 13, 28, 32, 32, 13, 0.4},
		{"reduced gap", 14, 28, 31, 31, 14, 0.4},
		{"minimal overlap", 16, 28, 27, 27, 16, 0.4},
		{"full row", 25, 28, 17, 17, 25, 0.4},
		{"wrap without overlap", 26, 28, 32, 32, 13, 0.4},
		{"short final row", 31, 28, 27, 29, 16, 0.4},
		{"four rows", 76, 28, 22, 22, 19, 0.4},
		{"no overlap", 40, 28, 32, 32, 10, 0},
		{"dense", 100, 28, 8, 8, 50, 0.75},
		{"odd size", 26, 27, 31, 31, 13, 0.4},
		{"full width", 3, 440, 444, 444, 1, 0.4},
		{"tiny", 441, 1, 1, 2, 221, 0.99},
	} {
		t.Run(tc.name, func(t *testing.T) {
			list := make([]people.Person, tc.count)
			for i := range list {
				list[i] = people.Person{Login: fmt.Sprintf("person-%03d", i), AvatarB64: "data:image/png;base64,AA=="}
			}
			frag, err := (&people.Plugin{}).Render(nil, people.Data{
				Size: tc.size, MaxOverlap: tc.overlap,
				Sections: []people.Section{{Type: "following", Total: tc.count, People: list}},
			})
			require.NoError(t, err)
			sections := readRenderedSections(t, frag.Body)
			require.Len(t, sections, 1)
			require.Len(t, sections[0].Avatars, tc.count)
			var gotLogins, wantLogins []string
			for i, avatar := range sections[0].Avatars {
				step := tc.step
				if i/tc.cols == (tc.count-1)/tc.cols {
					step = tc.lastStep
				}
				require.Equal(t, (i%tc.cols)*step, avatar.X)
				require.Equal(t, 28+(i/tc.cols)*(tc.size+4), avatar.Y)
				require.Equal(t, tc.size, avatar.Width)
				require.Equal(t, tc.size, avatar.Height)
				require.LessOrEqual(t, avatar.X+avatar.Width, frag.Width)
				require.LessOrEqual(t, avatar.Y+avatar.Height, frag.Height)
				gotLogins = append(gotLogins, avatar.Title)
				wantLogins = append(wantLogins, list[i].Login)
			}
			require.ElementsMatch(t, wantLogins, gotLogins)
			rows := (tc.count + tc.cols - 1) / tc.cols
			require.Equal(t, 36+rows*(tc.size+4), frag.Height)
		})
	}
}

func TestRender_ShufflesEachSectionWithoutMutatingData(t *testing.T) {
	data := people.Data{Size: 28, MaxOverlap: 0.4}
	for _, kind := range []string{"followers", "following"} {
		section := people.Section{Type: kind, Total: 20}
		for i := range section.Total {
			section.People = append(section.People, people.Person{
				Login: fmt.Sprintf("%s-%02d", kind, i), AvatarB64: "data:image/png;base64,AA==",
			})
		}
		data.Sections = append(data.Sections, section)
	}
	before := slices.Clone(data.Sections)
	for i := range before {
		before[i].People = slices.Clone(before[i].People)
	}
	firstOrders := make([][]string, len(data.Sections))
	changed := make([]bool, len(data.Sections))
	for pass := range 5 {
		frag, err := (&people.Plugin{}).Render(nil, data)
		require.NoError(t, err)
		sections := readRenderedSections(t, frag.Body)
		require.Len(t, sections, len(data.Sections))
		for i, section := range sections {
			require.Equal(t, before[i].Type, section.Type)
			var gotLogins, wantLogins []string
			for _, avatar := range section.Avatars {
				gotLogins = append(gotLogins, avatar.Title)
			}
			for _, person := range before[i].People {
				wantLogins = append(wantLogins, person.Login)
			}
			require.ElementsMatch(t, wantLogins, gotLogins)
			if pass == 0 {
				firstOrders[i] = gotLogins
			} else if !slices.Equal(firstOrders[i], gotLogins) {
				changed[i] = true
			}
		}
		require.Equal(t, before, data.Sections)
	}
	for i := range changed {
		require.True(t, changed[i], data.Sections[i].Type+" should shuffle on each render")
	}
}

func TestDecodeConfig_MaxOverlap(t *testing.T) {
	p := &people.Plugin{}
	raw, err := p.DecodeConfig(nil)
	require.NoError(t, err)
	require.Equal(t, 0.4, raw.(people.Config).MaxOverlap)
	for _, value := range []any{0, int64(0), 0.25, 0.99} {
		_, err := p.DecodeConfig(map[string]any{"max_overlap": value})
		require.NoError(t, err)
	}
	for _, value := range []any{-0.1, 1, math.NaN(), math.Inf(1), "0.4"} {
		_, err := p.DecodeConfig(map[string]any{"max_overlap": value})
		require.ErrorContains(t, err, "max_overlap")
	}
}

func TestRender_RejectsInvalidLayout(t *testing.T) {
	for _, data := range []people.Data{
		{Size: 0}, {Size: -1}, {Size: 441},
		{Size: 28, MaxOverlap: -0.1}, {Size: 28, MaxOverlap: 1},
		{Size: 28, MaxOverlap: math.NaN()}, {Size: 28, MaxOverlap: math.Inf(1)},
	} {
		_, err := (&people.Plugin{}).Render(nil, data)
		require.Error(t, err)
	}
}
