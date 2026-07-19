package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/api"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/utils/logging"
	"github.com/jakechorley/ilford-drop-in/web"
)

func main() {
	env := flag.String("env", "", "Environment (required: test, prod, etc.)")
	flag.Parse()

	if *env == "" {
		fmt.Fprintln(os.Stderr, "required flag \"env\" not set")
		os.Exit(1)
	}

	if err := run(*env); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(env string) error {
	logger, err := logging.InitLogger(env)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Sync()

	logger.Info("Starting server", zap.String("environment", env))

	cfg, err := config.LoadWithEnv(env)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if cfg.Server == nil {
		return fmt.Errorf("server config missing: add server.port to drop_in_config.%s.yaml", env)
	}

	webOAuthCfg, err := config.LoadOAuthClientWebWithEnv(env)
	if err != nil {
		return fmt.Errorf("failed to load web OAuth client config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The volunteer roster is fetched from the sheet with the server's own
	// service account: once at startup (below) and again on each admin sync. The
	// admin only triggers the refetch — no token is taken from them.
	serviceAccount, err := config.LoadServiceAccountWithEnv(env)
	if err != nil {
		return fmt.Errorf("failed to load service account: %w", err)
	}

	volunteers := api.NewVolunteerStore()
	syncVolunteers := func(ctx context.Context) error {
		client, err := sheetsclient.NewClientFromServiceAccount(ctx, serviceAccount.JSON)
		if err != nil {
			return fmt.Errorf("failed to build sheets client for sync: %w", err)
		}
		fetched, err := client.ListVolunteers(cfg)
		if err != nil {
			return fmt.Errorf("failed to fetch volunteers for sync: %w", err)
		}
		volunteers.Replace(fetched)
		logger.Info("Volunteer roster synced", zap.Int("count", len(fetched)))
		return nil
	}

	// Populate the roster at startup so reads work before any admin syncs. A
	// failure here (transient Sheets outage, say) is not fatal: the server boots
	// with an empty roster and an admin can retry via the sync button, matching
	// the store's "degrade to no volunteers" behaviour.
	if err := syncVolunteers(ctx); err != nil {
		logger.Warn("Failed to populate volunteer roster at startup; starting empty", zap.Error(err))
	}

	authenticator, err := api.NewAuthenticator(ctx, webOAuthCfg, cfg.Server, env, logger, syncVolunteers)
	if err != nil {
		return fmt.Errorf("failed to create authenticator: %w", err)
	}

	database, err := db.NewDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer database.Close()
	if err := database.RunMigrations(ctx); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	handler := api.NewHandler(database, volunteers, cfg, authenticator, web.Dist(), logger)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	logger.Info("Server listening", zap.Int("port", cfg.Server.Port))

	select {
	case err := <-serverErr:
		return fmt.Errorf("server failed: %w", err)
	case <-ctx.Done():
		logger.Info("Shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown failed: %w", err)
		}
	}

	logger.Info("Server stopped")
	return nil
}
