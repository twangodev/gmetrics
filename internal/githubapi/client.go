package githubapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/google/go-github/v66/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

type Config struct {
	Token          string
	RESTBaseURL    string       // must end with "/"
	GraphQLBaseURL string       // full URL, e.g. ".../graphql"
	HTTPClient     *http.Client // wrapped with OAuth; nil uses the default transport
}

type Clients struct {
	REST    *github.Client
	GraphQL *githubv4.Client
}

func New(ctx context.Context, cfg Config) (*Clients, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("github token required")
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.Token})

	var httpClient *http.Client
	if cfg.HTTPClient != nil {
		httpClient = &http.Client{
			Transport: &oauth2.Transport{
				Source: ts,
				Base:   rateLimitedTransport(cfg.HTTPClient.Transport),
			},
		}
	} else {
		httpClient = &http.Client{
			Transport: &oauth2.Transport{
				Source: ts,
				Base:   rateLimitedTransport(nil),
			},
		}
	}

	rest := github.NewClient(httpClient)
	if cfg.RESTBaseURL != "" {
		u, err := url.Parse(cfg.RESTBaseURL)
		if err != nil {
			return nil, fmt.Errorf("parse rest base url: %w", err)
		}
		rest.BaseURL = u
	}

	var gql *githubv4.Client
	if cfg.GraphQLBaseURL != "" {
		gql = githubv4.NewEnterpriseClient(cfg.GraphQLBaseURL, httpClient)
	} else {
		gql = githubv4.NewClient(httpClient)
	}

	return &Clients{REST: rest, GraphQL: gql}, nil
}
