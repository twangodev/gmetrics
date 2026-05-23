package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/twangodev/gmetrics/internal/config"
	"github.com/twangodev/gmetrics/internal/githubapi"
	"github.com/twangodev/gmetrics/internal/httpx"
	gmlog "github.com/twangodev/gmetrics/internal/log"
	"github.com/twangodev/gmetrics/internal/metrics"
	"github.com/twangodev/gmetrics/internal/plugin"
	"github.com/twangodev/gmetrics/internal/render"

	// Side-effect plugin registration:
	_ "github.com/twangodev/gmetrics/internal/plugins/base"
	_ "github.com/twangodev/gmetrics/internal/plugins/languages"
	_ "github.com/twangodev/gmetrics/internal/plugins/music"
	_ "github.com/twangodev/gmetrics/internal/plugins/people"
	_ "github.com/twangodev/gmetrics/internal/plugins/steam"
	_ "github.com/twangodev/gmetrics/internal/plugins/wakatime"
)

var (
	renderCfgPath string
	renderOutPath string
	renderStrict  bool
	renderVerbose bool
)

var renderCmd = &cobra.Command{
	Use:   "render",
	Short: "Render a metrics SVG from a YAML config and/or INPUT_* env vars",
	RunE:  runRender,
}

func init() {
	renderCmd.Flags().StringVarP(&renderCfgPath, "config", "c", "", "Path to YAML config file (optional; env vars overlay)")
	renderCmd.Flags().StringVarP(&renderOutPath, "output", "o", "", "Output SVG path (overrides filename in config)")
	renderCmd.Flags().BoolVar(&renderStrict, "strict", false, "Fail the whole render on any plugin error")
	renderCmd.Flags().BoolVarP(&renderVerbose, "verbose", "v", false, "Verbose (debug-level) logging")
	rootCmd.AddCommand(renderCmd)
}

func runRender(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// 1. Logger.
	lvl := slog.LevelInfo
	if renderVerbose {
		lvl = slog.LevelDebug
	}
	logger := gmlog.NewLogger(gmlog.Options{Level: lvl})

	// 2. Config (combined: file + env, env wins). Empty filePath falls back
	// to env-only mode, matching how the GitHub Action invokes us.
	cfg, err := config.LoadCombined(renderCfgPath, os.Environ())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if renderOutPath != "" {
		cfg.Filename = renderOutPath
	}
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	// 3. HTTP client with retries + light rate limiting.
	hc := httpx.New(httpx.Config{
		MaxRetries:    3,
		RetryWait:     500 * time.Millisecond,
		RatePerSecond: 5,
		Burst:         5,
		UserAgent:     "gmetrics/0.1",
	})

	// 4. GitHub clients. Surface a friendly error when no token is set
	// rather than letting githubapi.New return its generic message.
	if cfg.GitHub.Token == "" {
		return fmt.Errorf("github token required (set github.token in config or INPUT_TOKEN env)")
	}
	clients, err := githubapi.New(ctx, githubapi.Config{
		Token:      cfg.GitHub.Token,
		HTTPClient: hc.HTTPClient(),
	})
	if err != nil {
		return fmt.Errorf("github client: %w", err)
	}

	// 5. Plugin env.
	env := &plugin.Env{
		Login:   cfg.User,
		REST:    clients.REST,
		GraphQL: clients.GraphQL,
		HTTP:    hc.HTTPClient(),
		Log:     logger,
	}

	// 6. Engine.
	engine := &metrics.Engine{Env: env, Strict: renderStrict}
	frags, err := engine.Render(ctx, cfg)
	if err != nil {
		return fmt.Errorf("engine render: %w", err)
	}

	// 7. Outer-frame composition.
	framer := render.NewFramer(render.Options{Width: 480, Title: cfg.User})
	svg, err := framer.Compose(frags)
	if err != nil {
		return fmt.Errorf("compose: %w", err)
	}

	// 8. Write SVG.
	if err := os.WriteFile(cfg.Filename, []byte(svg), 0o644); err != nil {
		return fmt.Errorf("write svg: %w", err)
	}
	logger.Info("wrote svg", "path", cfg.Filename, "bytes", len(svg), "fragments", len(frags))
	return nil
}
