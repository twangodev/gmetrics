package languages

import (
	"context"
	"fmt"
	"sort"

	"github.com/shurcooL/githubv4"
	"github.com/twangodev/gmetrics/internal/plugin"
)

// repoLangsQuery walks the user's repositories one page at a time, picking
// up just the per-repository languages aggregate the v3+v4 API surfaces:
// GitHub pre-computes byte counts via Linguist server-side and exposes them
// through this edge. That makes the non-indepth path very cheap — typically
// one GraphQL hop per `first: batch` repos, walking pageInfo until exhausted.
//
// The languages edge can itself paginate (a repo with >100 languages) but
// that's rare in practice; we fetch the first 50 per repo and don't paginate
// inside repos. If a repo somehow has more, the long tail just doesn't show
// up in the legend, which is fine because we cap to Limit anyway.
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

// fetchGraphQL walks the user's repositories, accumulates language byte
// counts (and the per-language color the GraphQL API returned), filters
// cfg.Ignored, sorts by Bytes desc, caps to cfg.Limit, computes Percent.
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

	// pageGuard caps the number of GraphQL hops we'll make in case GitHub
	// reports HasNextPage indefinitely (it won't, but defense in depth
	// keeps a misbehaving mock from looping forever in tests).
	pageGuard := 1000
	for pageGuard > 0 {
		pageGuard--
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

// assemble takes the raw byte map (from either path), applies the ignored
// filter, sorts desc by bytes, caps to Limit (optionally rolling the tail
// into "Other"), computes percentages, and returns a fully-populated Data.
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
	// Stable sort: by descending bytes, then ascending name for determinism
	// when two languages tie (which happens routinely in synthesised test
	// data — e.g. two repos each contributing 1000 bytes of distinct langs).
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
		// Drop the smallest of the kept set so we still fit within limit
		// once "Other" is appended (matches upstream's behavior).
		if len(kept) == limit && len(kept) > 0 {
			rolled := kept[len(kept)-1].n
			kept = kept[:len(kept)-1]
			tail += rolled
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

// repoAffiliationsToEnum converts the human-readable affiliation strings
// ("owner", "collaborator", "organization_member") to the githubv4 enum
// values the GraphQL schema expects. Unknown values are dropped silently —
// the validator in config.go is responsible for catching them earlier.
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
