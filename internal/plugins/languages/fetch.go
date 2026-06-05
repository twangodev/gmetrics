package languages

import (
	"context"
	"fmt"
	"sort"

	"github.com/shurcooL/githubv4"
	"github.com/twangodev/gmetrics/internal/plugin"
)

// Bounds repository pagination against a backend that never clears HasNextPage.
const maxPaginationHops = 1000

type repoLangsQuery struct {
	User struct {
		Repositories struct {
			Nodes []struct {
				NameWithOwner githubv4.String
				IsFork        githubv4.Boolean
				Languages     struct {
					Edges []struct {
						Size githubv4.Int
						Node struct {
							Name  githubv4.String
							Color githubv4.String
						}
					}
				} `graphql:"languages(first: 50, orderBy: {field: SIZE, direction: DESC})"`
			}
			PageInfo struct {
				EndCursor   githubv4.String
				HasNextPage githubv4.Boolean
			}
		} `graphql:"repositories(first: $first, after: $cursor, ownerAffiliations: $affiliations, isFork: false)"`
	} `graphql:"user(login: $login)"`
}

func fetchGraphQL(ctx context.Context, env *plugin.Env, cfg Config) (Data, error) {
	if env.GraphQL == nil {
		return Data{}, fmt.Errorf("languages: fetch: env.GraphQL is nil")
	}
	login := env.Login
	if login == "" {
		login = env.User.Login
	}
	if login == "" {
		return Data{}, fmt.Errorf("languages: fetch: env.Login is empty")
	}

	affiliations := repoAffiliationsToEnum(cfg.RepoAffiliations)

	bytes := map[string]int{}
	colors := map[string]string{}

	vars := map[string]any{
		"login":        githubv4.String(login),
		"first":        githubv4.Int(cfg.RepoBatch),
		"cursor":       (*githubv4.String)(nil),
		"affiliations": affiliations,
	}

	remainingHops := maxPaginationHops
	for remainingHops > 0 {
		remainingHops--
		var q repoLangsQuery
		if err := env.GraphQL.Query(ctx, &q, vars); err != nil {
			return Data{}, fmt.Errorf("languages: fetch graphql: %w", err)
		}
		for _, repo := range q.User.Repositories.Nodes {
			if bool(repo.IsFork) {
				continue
			}
			for _, edge := range repo.Languages.Edges {
				name := string(edge.Node.Name)
				bytes[name] += int(edge.Size)
				if c := string(edge.Node.Color); c != "" {
					if _, ok := colors[name]; !ok {
						colors[name] = c
					}
				}
			}
		}
		if !bool(q.User.Repositories.PageInfo.HasNextPage) {
			break
		}
		vars["cursor"] = githubv4.NewString(q.User.Repositories.PageInfo.EndCursor)
	}

	return assemble(cfg, bytes, colors, false), nil
}

func assemble(cfg Config, bytes map[string]int, colors map[string]string, indepth bool) Data {
	type pair struct {
		name string
		n    int
	}
	pairs := make([]pair, 0, len(bytes))
	for name, n := range bytes {
		if isIgnored(name, cfg.Ignored) {
			continue
		}
		pairs = append(pairs, pair{name, n})
	}
	// Name tiebreak keeps ordering deterministic when byte counts are equal.
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].n != pairs[j].n {
			return pairs[i].n > pairs[j].n
		}
		return pairs[i].name < pairs[j].name
	})

	limit := cfg.Limit
	if limit <= 0 {
		limit = 8
	}

	var kept []pair
	var tail int
	if len(pairs) > limit {
		kept = pairs[:limit]
		for _, p := range pairs[limit:] {
			tail += p.n
		}
	} else {
		kept = pairs
	}

	if cfg.Other && tail > 0 {
		keptIsFull := len(kept) == limit && len(kept) > 0
		if keptIsFull {
			smallestKept := kept[len(kept)-1].n
			kept = kept[:len(kept)-1]
			tail += smallestKept
		}
		kept = append(kept, pair{name: "Other", n: tail})
	}

	total := 0
	for _, p := range kept {
		total += p.n
	}

	langs := make([]Lang, 0, len(kept))
	for _, p := range kept {
		percent := 0.0
		if total > 0 {
			percent = float64(p.n) / float64(total)
		}
		langs = append(langs, Lang{
			Name:    p.name,
			Color:   colorFor(p.name, colors),
			Bytes:   p.n,
			Percent: percent,
		})
	}

	sections := cfg.Sections
	if len(sections) == 0 {
		sections = []string{"most-used"}
	}
	details := cfg.Details
	if len(details) == 0 {
		details = []string{"percentage"}
	}

	return Data{
		Sections: sections,
		Details:  details,
		Total:    total,
		Langs:    langs,
		Indepth:  indepth,
	}
}

func repoAffiliationsToEnum(in []string) []githubv4.RepositoryAffiliation {
	if len(in) == 0 {
		return []githubv4.RepositoryAffiliation{githubv4.RepositoryAffiliationOwner}
	}
	out := make([]githubv4.RepositoryAffiliation, 0, len(in))
	for _, s := range in {
		switch s {
		case "owner":
			out = append(out, githubv4.RepositoryAffiliationOwner)
		case "collaborator":
			out = append(out, githubv4.RepositoryAffiliationCollaborator)
		case "organization_member":
			out = append(out, githubv4.RepositoryAffiliationOrganizationMember)
		}
	}
	if len(out) == 0 {
		return []githubv4.RepositoryAffiliation{githubv4.RepositoryAffiliationOwner}
	}
	return out
}
