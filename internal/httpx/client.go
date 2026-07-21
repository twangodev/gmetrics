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
	MaxRetries     int
	RetryWait      time.Duration
	RequestTimeout time.Duration
	RatePerSecond  float64
	Burst          int
	UserAgent      string
}

type Client struct {
	httpClient *http.Client
}

type requestTransport struct {
	base      http.RoundTripper
	limiter   *rate.Limiter
	userAgent string
}

func (t *requestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.limiter != nil {
		if err := t.limiter.Wait(req.Context()); err != nil {
			return nil, err
		}
	}
	if t.userAgent != "" && req.Header.Get("User-Agent") == "" {
		req = req.Clone(req.Context())
		req.Header = req.Header.Clone()
		req.Header.Set("User-Agent", t.userAgent)
	}
	return t.base.RoundTrip(req)
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

	var limiter *rate.Limiter
	if cfg.RatePerSecond > 0 {
		burst := cfg.Burst
		if burst <= 0 {
			burst = defaultBurst
		}
		limiter = rate.NewLimiter(rate.Limit(cfg.RatePerSecond), burst)
	}

	// Wrap retryablehttp's underlying client so every attempt, including retries,
	// consumes a limiter token and receives the configured user agent.
	baseTransport := retryClient.HTTPClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	retryClient.HTTPClient.Transport = &requestTransport{
		base:      baseTransport,
		limiter:   limiter,
		userAgent: cfg.UserAgent,
	}
	httpClient := retryClient.StandardClient()
	httpClient.Timeout = cfg.RequestTimeout
	return &Client{httpClient: httpClient}
}

func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

func (c *Client) Get(url string) (*http.Response, error) {
	return c.Do(context.Background(), http.MethodGet, url, nil)
}

func (c *Client) Do(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}
