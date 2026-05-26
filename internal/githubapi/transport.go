package githubapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/gofri/go-github-ratelimit/v2/github_ratelimit"
	"golang.org/x/time/rate"
)

const searchRatePerMinute = 25

type searchThrottle struct {
	base    http.RoundTripper
	limiter *rate.Limiter
}

func (s *searchThrottle) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasPrefix(req.URL.Path, "/search/") {
		if err := s.limiter.Wait(req.Context()); err != nil {
			return nil, err
		}
	}
	return s.base.RoundTrip(req)
}

func rateLimitedTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	throttled := &searchThrottle{
		base:    base,
		limiter: rate.NewLimiter(rate.Every(time.Minute/searchRatePerMinute), 1),
	}
	return github_ratelimit.New(throttled)
}
