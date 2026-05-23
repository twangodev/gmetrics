package httpx

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClient_RetriesOn5xx(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(Config{
		MaxRetries: 3,
		RetryWait:  10 * time.Millisecond,
	})

	resp, err := c.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(3), atomic.LoadInt32(&count))
}

func TestClient_RateLimiterPaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(Config{
		RatePerSecond: 2,
		Burst:         2,
	})

	start := time.Now()
	for i := 0; i < 3; i++ {
		resp, err := c.Get(srv.URL)
		require.NoError(t, err)
		resp.Body.Close()
	}
	elapsed := time.Since(start)

	require.GreaterOrEqual(t, elapsed, 400*time.Millisecond,
		"expected total elapsed >= 400ms due to rate limiting, got %v", elapsed)
}
