package people

import (
	"context"
	"fmt"

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
	if env.GraphQL == nil {
		return nil, fmt.Errorf("people: fetch: env.GraphQL is nil")
	}

	login := env.Login
	if login == "" {
		login = env.User.Login
	}
	if login == "" {
		return nil, fmt.Errorf("people: fetch: env.Login is empty")
	}

	data := Data{Size: cfg.Size}

	for _, t := range cfg.Types {
		people, total, err := fetchType(ctx, env, login, t, cfg)
		if err != nil {
			return nil, fmt.Errorf("people: fetch %s: %w", t, err)
		}
		data.Sections = append(data.Sections, Section{Type: t, Total: total, People: people})
	}
	return data, nil
}

func fetchType(ctx context.Context, env *plugin.Env, login, t string, cfg Config) ([]Person, int, error) {
	const retinaScale = 2
	vars := map[string]any{
		"login": githubv4.String(login),
		"first": githubv4.Int(cfg.Limit),
		"size":  githubv4.Int(cfg.Size * retinaScale),
	}

	var nodes []struct {
		Login     githubv4.String
		AvatarURL githubv4.String
	}
	var upstreamTotal int

	switch t {
	case "followers":
		var q followersQuery
		if err := env.GraphQL.Query(ctx, &q, vars); err != nil {
			return nil, 0, err
		}
		upstreamTotal = int(q.User.Followers.TotalCount)
		for _, n := range q.User.Followers.Nodes {
			nodes = append(nodes, struct {
				Login     githubv4.String
				AvatarURL githubv4.String
			}{n.Login, n.AvatarURL})
		}
	case "following":
		var q followingQuery
		if err := env.GraphQL.Query(ctx, &q, vars); err != nil {
			return nil, 0, err
		}
		upstreamTotal = int(q.User.Following.TotalCount)
		for _, n := range q.User.Following.Nodes {
			nodes = append(nodes, struct {
				Login     githubv4.String
				AvatarURL githubv4.String
			}{n.Login, n.AvatarURL})
		}
	default:
		return nil, 0, fmt.Errorf("unsupported type %q", t)
	}

	out := make([]Person, 0, len(nodes))
	for _, n := range nodes {
		p := Person{Login: string(n.Login)}
		if env.HTTP != nil {
			b64, err := img.FetchAvatar(ctx, env.HTTP, string(n.AvatarURL))
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
	return out, upstreamTotal, nil
}
