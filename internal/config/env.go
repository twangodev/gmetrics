package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const inputsJSONEnv = "GMETRICS_INPUTS"

func inputsJSONEntries(environ []string) []string {
	var blob string
	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, inputsJSONEnv+"="); ok {
			blob = v
			break
		}
	}
	if blob == "" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal([]byte(blob), &m) != nil {
		return nil
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, "INPUT_"+strings.ToUpper(k)+"="+v)
	}
	return out
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ParseBool accepts the upstream lowlighter/metrics spellings; empty means false.
func ParseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "true", "on", "1":
		return true, nil
	case "no", "false", "off", "0", "":
		return false, nil
	}
	return false, fmt.Errorf("invalid bool: %q", s)
}

type envFieldKind int

const (
	envString envFieldKind = iota
	envBool
	envInt
	envCSV
)

type envMapping struct {
	dottedConfigPath string
	kind             envFieldKind
}

// Keyed by INPUT_* name minus prefix, uppercased; unknown names are ignored since Actions injects unrelated env vars.
var envMappings = map[string]envMapping{
	"USER":                            {"user", envString},
	"FILENAME":                        {"filename", envString},
	"TOKEN":                           {"github.token", envString},
	"OUTPUT_ACTION":                   {"output.action", envString},
	"BASE":                            {"base.sections", envCSV},
	"BASE_HIREABLE":                   {"base.hireable", envBool},
	"BASE_INDEPTH":                    {"base.indepth", envBool},
	"COMMITS_AUTHORING":               {"base.commits_authoring", envCSV},
	"REPOSITORIES":                    {"base.repositories.max", envInt},
	"REPOSITORIES_BATCH":              {"base.repositories.batch", envInt},
	"REPOSITORIES_AFFILIATIONS":       {"base.repositories.affiliations", envCSV},
	"REPOSITORIES_FORKS":              {"base.repositories.forks", envBool},
	"PLUGIN_LANGUAGES":                {"plugins.languages.enabled", envBool},
	"PLUGIN_LANGUAGES_SECTIONS":       {"plugins.languages.sections", envCSV},
	"PLUGIN_LANGUAGES_DETAILS":        {"plugins.languages.details", envCSV},
	"PLUGIN_LANGUAGES_IGNORED":        {"plugins.languages.ignored", envCSV},
	"PLUGIN_LANGUAGES_LIMIT":          {"plugins.languages.limit", envInt},
	"PLUGIN_LANGUAGES_OTHER":          {"plugins.languages.other", envBool},
	"PLUGIN_LANGUAGES_INDEPTH":        {"plugins.languages.indepth", envBool},
	"PLUGIN_LANGUAGES_INDEPTH_CACHE":  {"plugins.languages.indepth_cache", envString},
	"PLUGIN_PEOPLE":                   {"plugins.people.enabled", envBool},
	"PLUGIN_PEOPLE_TYPES":             {"plugins.people.types", envCSV},
	"PLUGIN_PEOPLE_LIMIT":             {"plugins.people.limit", envInt},
	"PLUGIN_PEOPLE_SIZE":              {"plugins.people.size", envInt},
	"PLUGIN_WAKATIME":                 {"plugins.wakatime.enabled", envBool},
	"PLUGIN_WAKATIME_TOKEN":           {"plugins.wakatime.token", envString},
	"PLUGIN_WAKATIME_URL":             {"plugins.wakatime.url", envString},
	"PLUGIN_WAKATIME_USER":            {"plugins.wakatime.user", envString},
	"PLUGIN_WAKATIME_DAYS":            {"plugins.wakatime.days", envInt},
	"PLUGIN_WAKATIME_SECTIONS":        {"plugins.wakatime.sections", envCSV},
	"PLUGIN_WAKATIME_LIMIT":           {"plugins.wakatime.limit", envInt},
	"PLUGIN_MUSIC":                    {"plugins.music.enabled", envBool},
	"PLUGIN_MUSIC_PROVIDER":           {"plugins.music.provider", envString},
	"PLUGIN_MUSIC_MODE":               {"plugins.music.mode", envString},
	"PLUGIN_MUSIC_USER":               {"plugins.music.user", envString},
	"PLUGIN_MUSIC_TOKEN":              {"plugins.music.token", envString},
	"PLUGIN_MUSIC_LIMIT":              {"plugins.music.limit", envInt},
	"PLUGIN_STEAM":                    {"plugins.steam.enabled", envBool},
	"PLUGIN_STEAM_TOKEN":              {"plugins.steam.token", envString},
	"PLUGIN_STEAM_USER":               {"plugins.steam.user", envString},
	"PLUGIN_STEAM_SECTIONS":           {"plugins.steam.sections", envCSV},
	"PLUGIN_STEAM_RECENT_GAMES_LIMIT": {"plugins.steam.recent_games_limit", envInt},
	"PLUGIN_STEAM_ACHIEVEMENTS_LIMIT": {"plugins.steam.achievements_limit", envInt},
}

func parseEnvValue(raw string, kind envFieldKind) (any, error) {
	switch kind {
	case envString:
		return raw, nil
	case envBool:
		return ParseBool(raw)
	case envInt:
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid int %q: %w", raw, err)
		}
		return n, nil
	case envCSV:
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			out = append(out, p)
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown env field kind %d", kind)
}

func setNested(root map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	cursor := root
	for i, part := range parts {
		if i == len(parts)-1 {
			cursor[part] = value
			return
		}
		next, ok := cursor[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			cursor[part] = next
		}
		cursor = next
	}
}

func envToYAML(environ []string) ([]byte, error) {
	root := map[string]any{}
	environ = append(inputsJSONEntries(environ), environ...)
	for _, kv := range environ {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		key := kv[:eq]
		val := kv[eq+1:]
		if !strings.HasPrefix(key, "INPUT_") {
			continue
		}
		name := strings.ToUpper(strings.TrimPrefix(key, "INPUT_"))
		m, ok := envMappings[name]
		if !ok {
			continue
		}
		parsed, err := parseEnvValue(val, m.kind)
		if err != nil {
			return nil, fmt.Errorf("env %s: %w", key, err)
		}
		setNested(root, m.dottedConfigPath, parsed)
	}
	if len(root) == 0 {
		return nil, nil
	}
	out, err := yaml.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("marshal env yaml: %w", err)
	}
	return out, nil
}
