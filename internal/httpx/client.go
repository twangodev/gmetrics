// Package httpx provides a small HTTP client wrapper with retries,
// rate limiting, and a configurable User-Agent. It is intended to be
// shared by callers (e.g. go-github) that need a *http.Client.
package httpx

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/time/rate"
)

// Config configures a Client.
type Config struct {
	MaxRetries    int           // retries on transient failures
	RetryWait     time.Duration // base backoff
	RatePerSecond float64       // 0 disables rate limiting
	Burst         int           // burst size; defaults to 1 if rate > 0
	UserAgent     string
}

// Client is an HTTP client with retries and optional rate limiting.
type Client struct {
	httpClient *http.Client
	limiter    *rate.Limiter
	userAgent  string
}

// New returns a new Client configured with the given Config.
func New(cfg Config) *Client {
	rc := retryablehttp.NewClient()
	rc.Logger = nil

	if cfg.MaxRetries > 0 {
		rc.RetryMax = cfg.MaxRetries
	}
	if cfg.RetryWait > 0 {
		rc.RetryWaitMin = cfg.RetryWait
		rc.RetryWaitMax = cfg.RetryWait * 8
	}

	c := &Client{
		httpClient: rc.StandardClient(),
		userAgent:  cfg.UserAgent,
	}

	if cfg.RatePerSecond > 0 {
		burst := cfg.Burst
		if burst <= 0 {
			burst = 1
		}
		c.limiter = rate.NewLimiter(rate.Limit(cfg.RatePerSecond), burst)
	}

	return c
}

// HTTPClient returns the underlying *http.Client. Consumers like go-github
// that take a *http.Client should use this to inherit retry behavior.
func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

// Get issues a GET request to the given URL using a background context.
func (c *Client) Get(url string) (*http.Response, error) {
	return c.Do(context.Background(), http.MethodGet, url, nil)
}

// Do issues an HTTP request with the configured method, URL, and body.
// When a rate limiter is configured, it waits for a token before sending.
// The configured User-Agent header is set when non-empty.
func (c *Client) Do(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	return c.httpClient.Do(req)
}
