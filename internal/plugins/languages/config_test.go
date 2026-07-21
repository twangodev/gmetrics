package languages

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeConfigRepoMax(t *testing.T) {
	raw, err := (&Plugin{}).DecodeConfig(map[string]any{"repo_max": 17})
	require.NoError(t, err)
	require.Equal(t, 17, raw.(Config).RepoMax)
}

func TestDecodeConfigRejectsInvalidRepoSelection(t *testing.T) {
	_, err := (&Plugin{}).DecodeConfig(map[string]any{"repo_max": -1})
	require.ErrorContains(t, err, "repo_max")

	_, err = (&Plugin{}).DecodeConfig(map[string]any{
		"repo_affiliations": []string{"owner", "friend"},
	})
	require.ErrorContains(t, err, "friend")
}
