package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twangodev/gmetrics/internal/config"
)

func TestLoad_YAMLBasic(t *testing.T) {
	yaml := []byte(`
user: alice
filename: out.svg
base:
  sections: [header, activity]
  hireable: true
plugins:
  languages:
    enabled: true
    sections: [most-used]
    ignored: [markdown]
`)
	cfg, err := config.LoadBytes(yaml)
	require.NoError(t, err)
	require.Equal(t, "alice", cfg.User)
	require.Equal(t, "out.svg", cfg.Filename)
	require.Equal(t, []string{"header", "activity"}, cfg.Base.Sections)
	require.True(t, cfg.Base.Hireable)
	require.True(t, cfg.Plugins.Languages.Enabled)
	require.Equal(t, []string{"most-used"}, cfg.Plugins.Languages.Sections)
	require.Equal(t, []string{"markdown"}, cfg.Plugins.Languages.Ignored)
}

func TestLoad_DefaultsApply(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(""))
	require.NoError(t, err)
	require.Equal(t, "github-metrics.svg", cfg.Filename)
	require.Equal(t, []string{"header", "activity", "community", "repositories", "metadata"}, cfg.Base.Sections)
	require.Equal(t, 8, cfg.Plugins.Languages.Limit)
	require.Equal(t, "none", cfg.Output.Action)
	// Spot-check more defaults.
	require.Equal(t, 100, cfg.Base.Repositories.Max)
	require.Equal(t, []string{"owner"}, cfg.Base.Repositories.Affiliations)
	require.Equal(t, 24, cfg.Plugins.People.Limit)
	require.Equal(t, "https://wakatime.com", cfg.Plugins.Wakatime.URL)
	require.Equal(t, "current", cfg.Plugins.Wakatime.User)
	require.Equal(t, 7, cfg.Plugins.Wakatime.Days)
	require.Equal(t, 4, cfg.Plugins.Music.Limit)
	require.Equal(t, 1, cfg.Plugins.Steam.RecentGamesLimit)
	require.Equal(t, 2, cfg.Plugins.Steam.AchievementsLimit)
}

func TestLoadFromEnv_BasicFields(t *testing.T) {
	t.Setenv("INPUT_USER", "bob")
	t.Setenv("INPUT_BASE_HIREABLE", "yes")
	t.Setenv("INPUT_PLUGIN_LANGUAGES_SECTIONS", "most-used,recently-used")
	t.Setenv("INPUT_PLUGIN_PEOPLE_LIMIT", "12")

	cfg, err := config.LoadFromEnv(os.Environ())
	require.NoError(t, err)
	require.Equal(t, "bob", cfg.User)
	require.True(t, cfg.Base.Hireable)
	require.Equal(t, []string{"most-used", "recently-used"}, cfg.Plugins.Languages.Sections)
	require.Equal(t, 12, cfg.Plugins.People.Limit)
}

func TestLoadFromEnv_LanguagesIndepthCache(t *testing.T) {
	t.Setenv("INPUT_PLUGIN_LANGUAGES_INDEPTH_CACHE", ".cache/x.json")

	cfg, err := config.LoadFromEnv(os.Environ())
	require.NoError(t, err)
	require.Equal(t, ".cache/x.json", cfg.Plugins.Languages.IndepthCache)
}

func TestLoadCombined_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "card.yaml")
	require.NoError(t, os.WriteFile(path, []byte("user: alice\nfilename: card.svg\n"), 0o644))

	t.Setenv("INPUT_USER", "bob")

	cfg, err := config.LoadCombined(path, os.Environ())
	require.NoError(t, err)
	require.Equal(t, "bob", cfg.User)
	// File-provided value still applies when env did not set it.
	require.Equal(t, "card.svg", cfg.Filename)
}

func TestParseBool_AllAccepted(t *testing.T) {
	for _, s := range []string{"yes", "Yes", "true", "True", "1", "on"} {
		b, err := config.ParseBool(s)
		require.NoError(t, err, "input %q", s)
		require.True(t, b, "expected %q to be true", s)
	}
	for _, s := range []string{"no", "No", "false", "False", "0", "off", ""} {
		b, err := config.ParseBool(s)
		require.NoError(t, err, "input %q", s)
		require.False(t, b, "expected %q to be false", s)
	}
}

func TestValidate_RejectsMissingUser(t *testing.T) {
	cfg := &config.Config{
		User:     "",
		Filename: "out.svg",
		Output:   config.OutputConfig{Action: "none"},
	}
	err := config.Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "user")
}

func TestValidate_RejectsNonLastfmMusic(t *testing.T) {
	cfg := &config.Config{
		User:     "alice",
		Filename: "out.svg",
		Output:   config.OutputConfig{Action: "none"},
		Plugins: config.PluginsConfig{
			Music: config.MusicConfig{Enabled: true, Provider: "spotify"},
		},
	}
	err := config.Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lastfm")
}

func TestValidate_RejectsNonNoneOutputAction(t *testing.T) {
	cfg := &config.Config{
		User:     "alice",
		Filename: "out.svg",
		Output:   config.OutputConfig{Action: "commit"},
	}
	err := config.Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "none")
}
