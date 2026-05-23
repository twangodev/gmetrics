package plugin

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/go-github/v66/github"
	"github.com/shurcooL/githubv4"
)

// Env is the runtime context handed to every plugin. It carries the
// authenticated GitHub clients, the target user's profile, and an HTTP
// client and logger plugins can use for ad-hoc work.
type Env struct {
	Login   string
	User    UserContext
	REST    *github.Client
	GraphQL *githubv4.Client
	HTTP    *http.Client
	Log     *slog.Logger
}

// UserContext describes the GitHub user (or organization) being rendered.
// It is populated once by the engine and shared with every plugin.
type UserContext struct {
	Login       string
	Name        string
	AvatarURL   string
	Bio         string
	CreatedAt   time.Time
	IsOrg       bool
	Followers   int
	Following   int
	PublicRepos int
	DatabaseID  int64 // numeric ID; used to construct <id>+<login>@users.noreply.github.com
}
