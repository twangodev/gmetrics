package people

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/go-github/v66/github"
	"github.com/shurcooL/githubv4"
	"github.com/twangodev/gmetrics/internal/img"
	"github.com/twangodev/gmetrics/internal/plugin"
)

type followersQuery struct {
	User struct {
		Followers struct {
			TotalCount githubv4.Int
			Nodes      []struct {
				Login     githubv4.String
				AvatarURL githubv4.String `graphql:"avatarUrl(size: $size)"`
			}
		} `graphql:"followers(first: $first)"`
	} `graphql:"user(login: $login)"`
}

type followingQuery struct {
	User struct {
		Following struct {
			TotalCount githubv4.Int
			Nodes      []struct {
				Login     githubv4.String
				AvatarURL githubv4.String `graphql:"avatarUrl(size: $size)"`
			}
		} `graphql:"following(first: $first)"`
	} `graphql:"user(login: $login)"`
}

func (*Plugin) Fetch(ctx context.Context, env *plugin.Env, raw any) (any, error) {
	cfg, ok := raw.(Config)
	if !ok {
		return nil, fmt.Errorf("people: fetch: want Config, got %T", raw)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if env == nil {
		return nil, fmt.Errorf("people: fetch: env is nil")
	}
	if env.GraphQL == nil && env.REST == nil {
		return nil, fmt.Errorf("people: fetch: GitHub client is nil")
	}

	login := env.Login
	if login == "" {
		login = env.User.Login
	}
	if login == "" {
		return nil, fmt.Errorf("people: fetch: env.Login is empty")
	}

	if env.REST != nil {
		return fetchREST(ctx, env, login, cfg)
	}

	data := Data{Size: cfg.Size}
	for _, t := range cfg.Types {
		people, total, err := fetchTypeGraphQL(ctx, env, login, t, cfg)
		if err != nil {
			return nil, fmt.Errorf("people: fetch %s: %w", t, err)
		}
		data.Sections = append(data.Sections, Section{Type: t, Total: total, People: people})
	}
	return data, nil
}

type personNode struct {
	login          string
	avatarURL      string
	isOrganization bool
}

func fetchREST(ctx context.Context, env *plugin.Env, login string, cfg Config) (Data, error) {
	profile, _, err := env.REST.Users.Get(ctx, login)
	if err != nil {
		return Data{}, fmt.Errorf("people: fetch profile: %w", err)
	}
	totals := map[string]int{
		"followers": profile.GetFollowers(),
		"following": profile.GetFollowing(),
	}

	data := Data{Size: cfg.Size}
	for _, t := range cfg.Types {
		people, err := fetchTypeREST(ctx, env, login, t, cfg)
		if err != nil {
			return Data{}, fmt.Errorf("people: fetch %s: %w", t, err)
		}
		data.Sections = append(data.Sections, Section{Type: t, Total: totals[t], People: people})
	}
	return data, nil
}

func fetchTypeREST(ctx context.Context, env *plugin.Env, login, t string, cfg Config) ([]Person, error) {
	opts := &github.ListOptions{PerPage: cfg.Limit}
	var users []*github.User
	var err error
	switch t {
	case "followers":
		users, _, err = env.REST.Users.ListFollowers(ctx, login, opts)
	case "following":
		users, _, err = env.REST.Users.ListFollowing(ctx, login, opts)
	default:
		return nil, fmt.Errorf("unsupported type %q", t)
	}
	if err != nil {
		return nil, err
	}

	nodes := make([]personNode, 0, len(users))
	for _, user := range users {
		nodes = append(nodes, personNode{
			login:          user.GetLogin(),
			avatarURL:      avatarURLAtSize(user.GetAvatarURL(), cfg.Size*2),
			isOrganization: strings.EqualFold(user.GetType(), "Organization"),
		})
	}
	return hydratePeople(ctx, env, nodes), nil
}

func fetchTypeGraphQL(ctx context.Context, env *plugin.Env, login, t string, cfg Config) ([]Person, int, error) {
	const retinaScale = 2
	vars := map[string]any{
		"login": githubv4.String(login),
		"first": githubv4.Int(cfg.Limit),
		"size":  githubv4.Int(cfg.Size * retinaScale),
	}

	var nodes []personNode
	var upstreamTotal int

	switch t {
	case "followers":
		var q followersQuery
		if err := env.GraphQL.Query(ctx, &q, vars); err != nil {
			return nil, 0, err
		}
		upstreamTotal = int(q.User.Followers.TotalCount)
		for _, n := range q.User.Followers.Nodes {
			nodes = append(nodes, personNode{login: string(n.Login), avatarURL: string(n.AvatarURL)})
		}
	case "following":
		var q followingQuery
		if err := env.GraphQL.Query(ctx, &q, vars); err != nil {
			return nil, 0, err
		}
		upstreamTotal = int(q.User.Following.TotalCount)
		for _, n := range q.User.Following.Nodes {
			nodes = append(nodes, personNode{login: string(n.Login), avatarURL: string(n.AvatarURL)})
		}
	default:
		return nil, 0, fmt.Errorf("unsupported type %q", t)
	}

	return hydratePeople(ctx, env, nodes), upstreamTotal, nil
}

func hydratePeople(ctx context.Context, env *plugin.Env, nodes []personNode) []Person {
	out := make([]Person, 0, len(nodes))
	for _, n := range nodes {
		p := Person{Login: n.login, IsOrganization: n.isOrganization}
		if env.HTTP != nil {
			b64, err := img.FetchAvatar(ctx, env.HTTP, n.avatarURL)
			if err != nil {
				if env.Log != nil {
					env.Log.Warn("people: avatar fetch failed",
						"login", p.Login, "err", err)
				}
			} else {
				p.AvatarB64 = b64
			}
		}
		out = append(out, p)
	}
	return out
}

func avatarURLAtSize(raw string, size int) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("s", strconv.Itoa(size))
	u.RawQuery = q.Encode()
	return u.String()
}
