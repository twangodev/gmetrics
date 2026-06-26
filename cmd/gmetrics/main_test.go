package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestRun_RecoversPanicFromCommand(t *testing.T) {
	boom := &cobra.Command{
		Use:  "boom",
		RunE: func(cmd *cobra.Command, args []string) error { panic("kaboom") },
	}
	rootCmd.AddCommand(boom)
	rootCmd.SetArgs([]string{"boom"})
	t.Cleanup(func() {
		rootCmd.RemoveCommand(boom)
		rootCmd.SetArgs(nil)
	})

	err := run()
	require.Error(t, err, "an escaped panic must be converted to an error, not crash the process")
	require.Contains(t, err.Error(), "fatal: panic:")
	require.Contains(t, err.Error(), "kaboom")
}
