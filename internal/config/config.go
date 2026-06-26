// Package config loads the gmetrics config, merging compiled-in defaults,
// then a YAML file, then INPUT_* env vars (later sources win).
package config

import (
	"fmt"

	koanfyaml "github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	User     string        `koanf:"user"`
	Filename string        `koanf:"filename"`
	Base     BaseConfig    `koanf:"base"`
	Plugins  PluginsConfig `koanf:"plugins"`
	GitHub   GitHubConfig  `koanf:"github"`
	Output   OutputConfig  `koanf:"output"`
}

type BaseConfig struct {
	Sections         []string  `koanf:"sections"`
	Hireable         bool      `koanf:"hireable"`
	Indepth          bool      `koanf:"indepth"`
	CommitsAuthoring []string  `koanf:"commits_authoring"`
	Repositories     RepoFetch `koanf:"repositories"`
}

type RepoFetch struct {
	Affiliations []string `koanf:"affiliations"`
	Max          int      `koanf:"max"`
	Batch        int      `koanf:"batch"`
	Forks        bool     `koanf:"forks"`
}

type PluginsConfig struct {
	Errors    PluginErrorsConfig `koanf:"errors"`
	Languages LanguagesConfig    `koanf:"languages"`
	People    PeopleConfig       `koanf:"people"`
	Wakatime  WakatimeConfig     `koanf:"wakatime"`
	Music     MusicConfig        `koanf:"music"`
	Steam     SteamConfig        `koanf:"steam"`
}

type PluginErrorsConfig struct {
	Fatal bool `koanf:"fatal"`
}

type LanguagesConfig struct {
	Enabled      bool     `koanf:"enabled"`
	Sections     []string `koanf:"sections"`
	Details      []string `koanf:"details"`
	Ignored      []string `koanf:"ignored"`
	Limit        int      `koanf:"limit"`
	Other        bool     `koanf:"other"`
	Indepth      bool     `koanf:"indepth"`
	IndepthCache string   `koanf:"indepth_cache"`
}

type PeopleConfig struct {
	Enabled bool     `koanf:"enabled"`
	Types   []string `koanf:"types"`
	Limit   int      `koanf:"limit"`
	Size    int      `koanf:"size"`
}

type WakatimeConfig struct {
	Enabled  bool     `koanf:"enabled"`
	Token    string   `koanf:"token"`
	URL      string   `koanf:"url"`
	User     string   `koanf:"user"`
	Days     int      `koanf:"days"`
	Sections []string `koanf:"sections"`
	Limit    int      `koanf:"limit"`
}

type MusicConfig struct {
	Enabled  bool   `koanf:"enabled"`
	Provider string `koanf:"provider"`
	Mode     string `koanf:"mode"`
	User     string `koanf:"user"`
	Token    string `koanf:"token"`
	Limit    int    `koanf:"limit"`
}

type SteamConfig struct {
	Enabled           bool     `koanf:"enabled"`
	Token             string   `koanf:"token"`
	User              string   `koanf:"user"`
	Sections          []string `koanf:"sections"`
	RecentGamesLimit  int      `koanf:"recent_games_limit"`
	AchievementsLimit int      `koanf:"achievements_limit"`
}

type GitHubConfig struct {
	Token string `koanf:"token"`
}

type OutputConfig struct {
	Action string `koanf:"action"`
}

const defaultsYAML = `filename: github-metrics.svg
base:
  sections: [header, activity, community, repositories, metadata]
  hireable: false
  indepth: false
  repositories:
    affiliations: [owner]
    max: 100
    batch: 100
    forks: false
plugins:
  errors:
    fatal: false
  languages:
    enabled: false
    sections: [most-used]
    limit: 8
    other: false
    indepth: false
  people:
    enabled: false
    types: [followers, following]
    limit: 24
    size: 28
  wakatime:
    enabled: false
    url: https://wakatime.com
    user: current
    days: 7
    limit: 5
  music:
    enabled: false
    limit: 4
  steam:
    enabled: false
    sections: [player, most-played, recently-played]
    recent_games_limit: 1
    achievements_limit: 2
output:
  action: none
`

func newKoanfWithDefaults() (*koanf.Koanf, error) {
	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider([]byte(defaultsYAML)), koanfyaml.Parser()); err != nil {
		return nil, fmt.Errorf("load defaults: %w", err)
	}
	return k, nil
}

func unmarshal(k *koanf.Koanf) (*Config, error) {
	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &cfg, nil
}

func LoadBytes(yamlSrc []byte) (*Config, error) {
	k, err := newKoanfWithDefaults()
	if err != nil {
		return nil, err
	}
	if len(yamlSrc) > 0 {
		if err := k.Load(rawbytes.Provider(yamlSrc), koanfyaml.Parser()); err != nil {
			return nil, fmt.Errorf("parse yaml: %w", err)
		}
	}
	return unmarshal(k)
}

func LoadFile(path string) (*Config, error) {
	b, err := readFile(path)
	if err != nil {
		return nil, err
	}
	return LoadBytes(b)
}

// Non-INPUT_* entries are ignored: GH Actions injects unrelated env vars.
func LoadFromEnv(environ []string) (*Config, error) {
	yamlBytes, err := envToYAML(environ)
	if err != nil {
		return nil, err
	}
	return LoadBytes(yamlBytes)
}

// LoadCombined layers env-derived YAML over the file, so env wins on conflict.
func LoadCombined(filePath string, environ []string) (*Config, error) {
	k, err := newKoanfWithDefaults()
	if err != nil {
		return nil, err
	}
	if filePath != "" {
		b, err := readFile(filePath)
		if err != nil {
			return nil, err
		}
		if len(b) > 0 {
			if err := k.Load(rawbytes.Provider(b), koanfyaml.Parser()); err != nil {
				return nil, fmt.Errorf("parse yaml %s: %w", filePath, err)
			}
		}
	}
	envBytes, err := envToYAML(environ)
	if err != nil {
		return nil, err
	}
	if len(envBytes) > 0 {
		if err := k.Load(rawbytes.Provider(envBytes), koanfyaml.Parser()); err != nil {
			return nil, fmt.Errorf("apply env overlay: %w", err)
		}
	}
	return unmarshal(k)
}

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.User == "" {
		return fmt.Errorf("user is required")
	}
	if cfg.Filename == "" {
		return fmt.Errorf("filename is required")
	}
	if cfg.Plugins.Music.Enabled && cfg.Plugins.Music.Provider != "lastfm" {
		return fmt.Errorf("only music provider 'lastfm' is supported in v1; got %q", cfg.Plugins.Music.Provider)
	}
	if cfg.Output.Action != "none" {
		return fmt.Errorf("only output.action 'none' is supported in v1; got %q", cfg.Output.Action)
	}
	return nil
}
