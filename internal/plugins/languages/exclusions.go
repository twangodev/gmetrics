package languages

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

const (
	exclusionFileName      = "exclusion.toml"
	exclusionPathEnv       = "GMETRICS_EXCLUSION_PATH"
	installedExclusionPath = "/usr/local/share/gmetrics/exclusion.toml"
)

type exclusionFile struct {
	Exclude []string `toml:"exclude"`
}

type exclusionRules struct {
	patterns []string
}

var (
	exclusionOnce  sync.Once
	defaultExclude exclusionRules
)

func currentExclusionRules() exclusionRules {
	exclusionOnce.Do(func() {
		defaultExclude = loadCurrentExclusionRules()
	})
	return defaultExclude
}

func loadCurrentExclusionRules() exclusionRules {
	paths := []string{}
	if configured := strings.TrimSpace(os.Getenv(exclusionPathEnv)); configured != "" {
		paths = append(paths, configured)
	}
	paths = append(paths, exclusionFileName, installedExclusionPath)
	for _, candidate := range paths {
		rules, err := loadExclusionRules(candidate)
		if err == nil {
			return rules
		}
		if !os.IsNotExist(err) {
			break
		}
	}
	return exclusionRules{}
}

func loadExclusionRules(filePath string) (exclusionRules, error) {
	var config exclusionFile
	if _, err := toml.DecodeFile(filePath, &config); err != nil {
		return exclusionRules{}, err
	}
	patterns := make([]string, 0, len(config.Exclude))
	for _, pattern := range config.Exclude {
		pattern = strings.TrimSpace(filepath.ToSlash(pattern))
		if pattern != "" {
			patterns = append(patterns, pattern)
		}
	}
	return exclusionRules{patterns: patterns}, nil
}

func (r exclusionRules) matches(filePath string) bool {
	filePath = filepath.ToSlash(filepath.Clean(filePath))
	for _, pattern := range r.patterns {
		if matchesExclusionPattern(filePath, pattern) {
			return true
		}
	}
	return false
}

func matchesExclusionPattern(filePath, pattern string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	if strings.HasPrefix(pattern, "**/") && strings.HasSuffix(pattern, "/**") {
		segment := strings.TrimSuffix(strings.TrimPrefix(pattern, "**/"), "/**")
		return strings.Contains("/"+filePath+"/", "/"+segment+"/")
	}
	if strings.HasPrefix(pattern, "**/") {
		suffix := strings.TrimPrefix(pattern, "**/")
		matched, _ := path.Match(suffix, filepath.Base(filePath))
		return matched
	}
	matched, _ := path.Match(pattern, filePath)
	return matched
}

func excludedPath(filePath string) bool {
	return currentExclusionRules().matches(filePath)
}
