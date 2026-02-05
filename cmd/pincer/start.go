package pincer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/igorsilveira/pincer/pkg/telemetry"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Pincer gateway",
	RunE:  runStart,
}

func runStart(cmd *cobra.Command, args []string) error {
	path := cfgFile
	if path == "" {
		path = config.DefaultConfigPath()
	}

	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := config.EnsureDataDir(); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	logger := telemetry.SetupLogger(cfg.Log.Level, cfg.Log.Format, nil)
	logger.Info("starting pincer gateway",
		slog.String("version", version),
		slog.Int("port", cfg.Gateway.Port),
		slog.String("bind", cfg.Gateway.Bind),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	<-ctx.Done()
	logger.Info("shutting down")

	return nil
}
