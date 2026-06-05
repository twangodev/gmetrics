package plugin

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/google/go-github/v66/github"
	"github.com/shurcooL/githubv4"
)

type Env struct {
	Login   string
	User    UserContext
	Token   string
	REST    *github.Client
	GraphQL *githubv4.Client
	HTTP    *http.Client
	Log     *slog.Logger
}

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
	DatabaseID  int64
}
