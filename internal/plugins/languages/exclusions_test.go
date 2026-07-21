package languages

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExclusionPatternsMatchImportedAssets(t *testing.T) {
	rules := exclusionRules{patterns: []string{
		"**/editor/static/**",
		"**/*.min.js",
	}}
	require.True(t, rules.matches("lab10/editor/static/ace/src-min-noconflict/ace.js"))
	require.True(t, rules.matches("dist/app.min.js"))
	require.False(t, rules.matches("src/editor/app.js"))
}

func TestLoadExclusionRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exclusion.toml")
	require.NoError(t, os.WriteFile(path, []byte("exclude = [\"**/generated/**\", \"**/*.min.js\"]\n"), 0o644))

	rules, err := loadExclusionRules(path)
	require.NoError(t, err)
	require.True(t, rules.matches("src/generated/output.js"))
	require.True(t, rules.matches("dist/app.min.js"))
}
