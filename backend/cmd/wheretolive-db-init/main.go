// Command wheretolive-db-init ensures the configured dedicated database exists before migrations.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/psmMRFP/WhereToLive/backend/internal/platform/config"
	"github.com/psmMRFP/WhereToLive/backend/internal/platform/database"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.DatabaseFromEnv()
	if err != nil {
		logger.Error("load database configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, created, err := database.Open(ctx, cfg.URL, cfg.AutoCreate)
	if err != nil {
		logger.Error("initialize application database", "error", err)
		os.Exit(1)
	}
	pool.Close()
	logger.Info("application database ready for migrations", "created", created)
}
