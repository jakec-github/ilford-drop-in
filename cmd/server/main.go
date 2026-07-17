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
)

const volunteerCacheTTL = 5 * time.Minute

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

	oauthCfg, err := config.LoadOAuthClientWithEnv(env)
	if err != nil {
		return fmt.Errorf("failed to load OAuth client config: %w", err)
	}

	webOAuthCfg, err := config.LoadOAuthClientWebWithEnv(env)
	if err != nil {
		return fmt.Errorf("failed to load web OAuth client config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sheetsClient, err := sheetsclient.NewClient(ctx, oauthCfg, env)
	if err != nil {
		return fmt.Errorf("failed to create sheets client: %w", err)
	}

	authenticator, err := api.NewAuthenticator(ctx, webOAuthCfg, cfg.Server, env, logger)
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

	volunteers := api.NewCachingVolunteerClient(sheetsClient, volunteerCacheTTL)
	handler := api.NewHandler(database, volunteers, cfg, authenticator, logger)

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
