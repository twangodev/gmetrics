package languages

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeFileString(path, s string) error {
	return os.WriteFile(path, []byte(s), 0o644)
}

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	c := newCache("hash-A")
	c.Repos["o/r"] = repoEntry{HeadSHA: "abc", PushedAt: "t1", Bytes: map[string]int{"Go": 10}, Commits: 1, Files: 1, Lines: 10}
	if err := saveCache(path, c); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("cache permissions = %o, want 644", got)
	}
	got := loadCache(path)
	if got.PredHash != "hash-A" || got.Repos["o/r"].HeadSHA != "abc" || got.Repos["o/r"].Bytes["Go"] != 10 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestLoadCacheMissingOrCorruptReturnsEmpty(t *testing.T) {
	if got := loadCache(filepath.Join(t.TempDir(), "nope.json")); len(got.Repos) != 0 {
		t.Fatal("missing file should yield empty cache")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := writeFileString(bad, "{not json"); err != nil {
		t.Fatal(err)
	}
	if got := loadCache(bad); len(got.Repos) != 0 {
		t.Fatal("corrupt file should yield empty cache")
	}
}

func TestLoadCacheInvalidatesOlderComputationVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.json")
	old := fmt.Sprintf(`{"version":%d,"predHash":"old","repos":{"o/r":{"bytes":{"Go":10}}}}`, cacheVersion-1)
	if err := writeFileString(path, old); err != nil {
		t.Fatal(err)
	}

	got := loadCache(path)
	if len(got.Repos) != 0 || got.Version != cacheVersion {
		t.Fatalf("old computation cache must be invalidated: %+v", got)
	}
}

func TestPredHashStableAndOrderInsensitive(t *testing.T) {
	if predHash([]string{"a", "b"}) != predHash([]string{"b", "a"}) {
		t.Fatal("predHash must be order-insensitive")
	}
	if predHash([]string{"a"}) == predHash([]string{"a", "b"}) {
		t.Fatal("different predicate sets must differ")
	}
}

func TestPruneDropsUnseenRepos(t *testing.T) {
	c := newCache("h")
	c.Repos["keep/me"] = repoEntry{}
	c.Repos["drop/me"] = repoEntry{}
	c.prune(map[string]struct{}{"keep/me": {}})
	if _, ok := c.Repos["drop/me"]; ok {
		t.Fatal("unseen repo not pruned")
	}
	if _, ok := c.Repos["keep/me"]; !ok {
		t.Fatal("seen repo wrongly pruned")
	}
}
