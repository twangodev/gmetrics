// Package httpx provides an HTTP client wrapper with retries and rate limiting.
package httpx

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/time/rate"
)

type Config struct {
	MaxRetries    int
	RetryWait     time.Duration
	RatePerSecond float64
	Burst         int
	UserAgent     string
}

type Client struct {
	httpClient *http.Client
	limiter    *rate.Limiter
	userAgent  string
}

const (
	maxBackoffMultiplier = 8
	defaultBurst         = 1
)

func New(cfg Config) *Client {
	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil

	if cfg.MaxRetries > 0 {
		retryClient.RetryMax = cfg.MaxRetries
	}
	if cfg.RetryWait > 0 {
		retryClient.RetryWaitMin = cfg.RetryWait
		retryClient.RetryWaitMax = cfg.RetryWait * maxBackoffMultiplier
	}

	c := &Client{
		httpClient: retryClient.StandardClient(),
		userAgent:  cfg.UserAgent,
	}

	if cfg.RatePerSecond > 0 {
		burst := cfg.Burst
		if burst <= 0 {
			burst = defaultBurst
		}
		c.limiter = rate.NewLimiter(rate.Limit(cfg.RatePerSecond), burst)
	}

	return c
}

func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

func (c *Client) Get(url string) (*http.Response, error) {
	return c.Do(context.Background(), http.MethodGet, url, nil)
}

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
