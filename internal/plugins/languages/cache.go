package languages

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const cacheVersion = 1

type repoEntry struct {
	HeadSHA  string         `json:"headSHA"`
	PushedAt string         `json:"pushedAt"`
	Bytes    map[string]int `json:"bytes"`
	Commits  int            `json:"commits"`
	Files    int            `json:"files"`
	Lines    int            `json:"lines"`
}

type cacheFile struct {
	Version  int                  `json:"version"`
	PredHash string               `json:"predHash"`
	Repos    map[string]repoEntry `json:"repos"`
}

func newCache(predHash string) *cacheFile {
	return &cacheFile{Version: cacheVersion, PredHash: predHash, Repos: map[string]repoEntry{}}
}

func loadCache(path string) *cacheFile {
	empty := newCache("")
	if path == "" {
		return empty
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var c cacheFile
	if json.Unmarshal(data, &c) != nil || c.Version != cacheVersion || c.Repos == nil {
		return empty
	}
	return &c
}

func saveCache(path string, c *cacheFile) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (c *cacheFile) prune(seen map[string]struct{}) {
	for k := range c.Repos {
		if _, ok := seen[k]; !ok {
			delete(c.Repos, k)
		}
	}
}

func predHash(preds []string) string {
	cp := append([]string(nil), preds...)
	sort.Strings(cp)
	sum := sha256.Sum256([]byte(strings.Join(cp, "\n")))
	return hex.EncodeToString(sum[:])
}
