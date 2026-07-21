package githubapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNew_RejectsMissingToken(t *testing.T) {
	t.Parallel()
	_, err := New(context.Background(), Config{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "token required")
}

func TestNewPreservesHTTPClientTimeout(t *testing.T) {
	const timeout = 37 * time.Second
	clients, err := New(context.Background(), Config{
		Token:      "test-token",
		HTTPClient: &http.Client{Timeout: timeout},
	})
	require.NoError(t, err)
	require.Equal(t, timeout, clients.REST.Client().Timeout)
}

func TestNew_AuthHeaderIsSet(t *testing.T) {
	t.Parallel()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/users/alice":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"login":"alice"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	clients, err := New(context.Background(), Config{
		Token:       "ghp_test_123",
		RESTBaseURL: srv.URL + "/",
	})
	require.NoError(t, err)
	require.NotNil(t, clients)

	user, _, err := clients.REST.Users.Get(context.Background(), "alice")
	require.NoError(t, err)
	require.NotNil(t, user)
	require.NotNil(t, user.Login)
	require.Equal(t, "alice", *user.Login)
	require.Equal(t, "Bearer ghp_test_123", gotAuth)
}

const rateLimitBody = `{"resources":{"core":{"limit":5000,"remaining":50,"reset":0},"graphql":{"limit":5000,"remaining":500,"reset":0},"search":{"limit":30,"remaining":5,"reset":0}},"rate":{"limit":5000,"remaining":50,"reset":0}}`

func newRateLimitServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rate_limit":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(rateLimitBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckQuota_RejectsBelowRequirement(t *testing.T) {
	t.Parallel()

	srv := newRateLimitServer(t)
	clients, err := New(context.Background(), Config{
		Token:       "ghp_test_123",
		RESTBaseURL: srv.URL + "/",
	})
	require.NoError(t, err)

	_, err = clients.CheckQuota(context.Background(), QuotaRequirement{REST: 200, GraphQL: 200, Search: 0})
	require.Error(t, err)
	require.Contains(t, err.Error(), "REST quota 50 below required 200")
}

func TestCheckQuota_OKWhenAllAbove(t *testing.T) {
	t.Parallel()

	srv := newRateLimitServer(t)
	clients, err := New(context.Background(), Config{
		Token:       "ghp_test_123",
		RESTBaseURL: srv.URL + "/",
	})
	require.NoError(t, err)

	q, err := clients.CheckQuota(context.Background(), QuotaRequirement{REST: 10, GraphQL: 10, Search: 0})
	require.NoError(t, err)
	require.Equal(t, 50, q.RESTRemaining)
}
