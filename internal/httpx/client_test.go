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

func TestHTTPClient_RateLimiterPaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(Config{RatePerSecond: 2, Burst: 2}).HTTPClient()
	start := time.Now()
	for i := 0; i < 3; i++ {
		resp, err := c.Get(srv.URL)
		require.NoError(t, err)
		resp.Body.Close()
	}
	require.GreaterOrEqual(t, time.Since(start), 400*time.Millisecond)
}

func TestHTTPClient_RateLimiterPacesRetries(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&count, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New(Config{
		MaxRetries:    2,
		RetryWait:     time.Millisecond,
		RatePerSecond: 20,
		Burst:         1,
	}).HTTPClient()
	start := time.Now()
	resp, err := c.Get(srv.URL)
	require.Error(t, err)
	if resp != nil {
		resp.Body.Close()
	}

	require.Equal(t, int32(3), atomic.LoadInt32(&count))
	require.GreaterOrEqual(t, time.Since(start), 90*time.Millisecond,
		"all retry attempts must be rate limited")
}

func TestHTTPClient_RequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := New(Config{MaxRetries: 1, RequestTimeout: 20 * time.Millisecond}).HTTPClient()
	_, err := c.Get(srv.URL)
	require.Error(t, err)
}
