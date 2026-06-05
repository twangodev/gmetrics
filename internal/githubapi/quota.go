package githubapi

import (
	"context"
	"fmt"
)

type Quota struct {
	RESTRemaining    int
	GraphQLRemaining int
	SearchRemaining  int
}

type QuotaRequirement struct {
	REST    int
	GraphQL int
	Search  int
}

func (c *Clients) CheckQuota(ctx context.Context, req QuotaRequirement) (Quota, error) {
	limits, _, err := c.REST.RateLimit.Get(ctx)
	if err != nil {
		return Quota{}, fmt.Errorf("query rate limits: %w", err)
	}
	q := Quota{
		RESTRemaining:    limits.Core.Remaining,
		GraphQLRemaining: limits.GraphQL.Remaining,
		SearchRemaining:  limits.Search.Remaining,
	}
	if q.RESTRemaining < req.REST {
		return q, fmt.Errorf("REST quota %d below required %d", q.RESTRemaining, req.REST)
	}
	if q.GraphQLRemaining < req.GraphQL {
		return q, fmt.Errorf("GraphQL quota %d below required %d", q.GraphQLRemaining, req.GraphQL)
	}
	if q.SearchRemaining < req.Search {
		return q, fmt.Errorf("Search quota %d below required %d", q.SearchRemaining, req.Search)
	}
	return q, nil
}
