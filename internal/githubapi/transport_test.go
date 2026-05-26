package githubapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestSearchThrottleOnlyThrottlesSearch(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &searchThrottle{
		base:    http.DefaultTransport,
		limiter: rate.NewLimiter(rate.Every(100*time.Millisecond), 1),
	}
	c := &http.Client{Transport: rt}

	start := time.Now()
	for i := 0; i < 3; i++ {
		resp, err := c.Get(srv.URL + "/search/commits?q=x")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("search requests not throttled: %v", elapsed)
	}

	start = time.Now()
	for i := 0; i < 3; i++ {
		resp, err := c.Get(srv.URL + "/user/repos")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("non-search requests should not be throttled: %v", elapsed)
	}
	if hits != 6 {
		t.Fatalf("want 6 hits, got %d", hits)
	}
}
