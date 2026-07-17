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
	"golang.org/x/oauth2"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/api"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/utils/logging"
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

	// The server holds no Sheets credential of its own: the volunteer roster is
	// populated by the admin-triggered sync using the admin's OAuth token. The
	// store starts empty and is filled on the first sync.
	volunteers := api.NewVolunteerStore()
	syncVolunteers := func(r *http.Request, token *oauth2.Token) error {
		client, err := sheetsclient.NewClientFromToken(r.Context(), token)
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
