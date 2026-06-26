package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "gmetrics",
	Short:         "Generate GitHub stats SVG infographics",
	SilenceErrors: true,
	SilenceUsage:  true,
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("fatal: panic: %v\n%s", r, debug.Stack())
		}
	}()
	return rootCmd.Execute()
}
