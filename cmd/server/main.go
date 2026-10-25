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
	"github.com/jakechorley/ilford-drop-in/internal/devmode"
	"github.com/jakechorley/ilford-drop-in/pkg/api"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/gmailclient"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/utils/logging"
	"github.com/jakechorley/ilford-drop-in/web"
)

func main() {
	env := flag.String("env", "", "Environment (required: test, prod, etc.)")
	// Overriding the port keeps one config file usable from several checkouts at
	// once: a git worktree runs its own stack on its own port (see
	// docs/agents/worktrees.md) without editing the config it shares.
	port := flag.Int("port", 0, "Override server.port from the config (0 keeps the configured port)")
	flag.Parse()

	if *env == "" {
		fmt.Fprintln(os.Stderr, "required flag \"env\" not set")
		os.Exit(1)
	}

	if err := run(*env, *port); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(env string, portOverride int) error {
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
		return fmt.Errorf("server config missing: add server.port to %s", config.EnvPath(env, config.ConfigFile))
	}
	if portOverride != 0 {
		cfg.Server.Port = portOverride
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The database comes up before anything that reads a roster. Roles are rows
	// now (ADR 0006), and the roster names the Roles a volunteer holds as
	// strings, so parsing one means reading them first.
	database, err := db.NewDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer database.Close()
	if err := database.RunMigrations(ctx); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// The organisation table must hold every configured id before anything is
	// written under one. On the first start after migration 030 this is also
	// where the rows from before Organisations existed are given to theirs.
	ids := make([]string, len(cfg.Organisations))
	for i, org := range cfg.Organisations {
		ids[i] = org.ID
	}
	adopted, err := database.SyncOrganisations(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to sync organisations: %w", err)
	}
	if adopted != "" {
		logger.Info("Existing rota adopted into an organisation", zap.String("organisation", adopted))
	}

	// Dev mode replaces every part of the Google dependency — the roster fetch,
	// the identity provider and outbound mail — so the server runs on a checkout
	// with no credentials at all. Config keeps it to the dev environment; this
	// branch and the one in newOrganisation are the only places it changes what
	// gets built.
	var shared sharedDeps
	var authenticator *api.Authenticator
	if cfg.DevMode != nil {
		logger.Warn("DEV MODE: Google is stubbed out — the roster comes from a file and login issues a session without verifying identity",
			zap.String("volunteersCSV", cfg.DevMode.VolunteersCSV),
			zap.String("organiserEmail", cfg.DevMode.OrganiserEmail),
			zap.String("rotaEditorEmail", cfg.DevMode.RotaEditorEmail))

		authenticator, err = api.NewStubAuthenticator(cfg.DevMode, cfg.Server, cfg.Organisations, logger)
		if err != nil {
			return fmt.Errorf("failed to create stub authenticator: %w", err)
		}
		shared.newMailer = api.NewStubMailer(logger)
	} else {
		webOAuthCfg, err := config.LoadOAuthClientWebWithEnv(env)
		if err != nil {
			return fmt.Errorf("failed to load web OAuth client config: %w", err)
		}

		// Every Organisation's roster is fetched, and its rota sheet written,
		// with the server's own service account. The Organiser only triggers the
		// refetch — no token is taken from them.
		serviceAccount, err := config.LoadServiceAccountWithEnv(env)
		if err != nil {
			return fmt.Errorf("failed to load service account: %w", err)
		}
		shared.serviceAccountJSON = serviceAccount.JSON

		authenticator, err = api.NewAuthenticator(ctx, webOAuthCfg, cfg.Server, env, logger)
		if err != nil {
			return fmt.Errorf("failed to create authenticator: %w", err)
		}

		// Availability emails go out as the signed-in Organiser, through a
		// gmail.send token they grant per send and the server never stores.
		// There is nothing to build here beyond the token itself — the client
		// is constructed inside the send and discarded with it.
		shared.newMailer = func(ctx context.Context, token *oauth2.Token) (services.GmailClient, error) {
			return gmailclient.NewClientFromToken(ctx, token)
		}
	}

	handlers := make([]*api.Handler, 0, len(cfg.Organisations))
	for i := range cfg.Organisations {
		handler, err := newOrganisation(ctx, &cfg.Organisations[i], cfg.DevMode, database, authenticator, shared, logger)
		if err != nil {
			return err
		}
		handlers = append(handlers, handler)
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           api.Mount(authenticator, handlers, web.Dist(), database, logger),
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

// sharedDeps is what every Organisation's handler is built from in common: the
// credentials are the deployment's, not any one drop-in's.
type sharedDeps struct {
	// serviceAccountJSON is empty in dev mode, where nothing reaches Google.
	serviceAccountJSON []byte
	newMailer          api.MailerFunc
}

// newOrganisation builds one Organisation's handler: its own bound store, its
// own roster cache, roster sync and rota publisher, so nothing one drop-in
// does reaches another's (ADR 0011).
func newOrganisation(
	ctx context.Context,
	org *config.Organisation,
	dev *config.DevModeConfig,
	database *db.DB,
	authenticator *api.Authenticator,
	shared sharedDeps,
	logger *zap.Logger,
) (*api.Handler, error) {
	store := database.ForOrganisation(org.ID)
	volunteers := api.NewVolunteerStore()
	baseLogger := logger
	logger = logger.With(zap.String("organisation", org.ID))

	var syncVolunteers api.VolunteerSyncFunc
	// Nil in dev mode: there is no sheet to publish to, and Trigger on a nil
	// publisher does nothing.
	var publisher *api.SheetPublisher
	if dev != nil {
		// Nothing else creates Roles yet, and the roster below is read against
		// them, so the seed has to run before the first sync.
		seeded, err := devmode.SeedRoles(ctx, store)
		if err != nil {
			return nil, fmt.Errorf("failed to seed dev roles for %s: %w", org.ID, err)
		}
		if seeded > 0 {
			logger.Info("DEV MODE: seeded roles", zap.Int("count", seeded))
		}

		// What those Roles are asked for, which is the other half of a rota the
		// dev stack can allocate. It runs after the Roles because a Seat names
		// one by id.
		seededShape, err := devmode.SeedDefaultShape(ctx, store)
		if err != nil {
			return nil, fmt.Errorf("failed to seed the dev default shape for %s: %w", org.ID, err)
		}
		if seededShape {
			logger.Info("DEV MODE: seeded the default shape")
		}

		// Same reason as the Roles: nothing else sets them, and a dev stack
		// that cannot allocate a rota is not the usable app it promises.
		seededDefaults, err := devmode.SeedRotaDefaults(ctx, store)
		if err != nil {
			return nil, fmt.Errorf("failed to seed dev rota defaults for %s: %w", org.ID, err)
		}
		if seededDefaults {
			logger.Info("DEV MODE: seeded rota defaults")
		}

		syncVolunteers = func(ctx context.Context) error {
			roles, err := services.RoleTable(ctx, store)
			if err != nil {
				return err
			}
			fetched, err := devmode.LoadVolunteers(dev.VolunteersCSV, roles)
			if err != nil {
				return err
			}
			volunteers.Replace(fetched)
			logger.Info("Volunteer roster loaded from CSV", zap.Int("count", len(fetched)))
			return nil
		}

		// Unlike a Sheets outage this is a local file that either exists or does
		// not, so an unreadable roster is a misconfiguration worth failing on
		// rather than something a later sync might fix.
		if err := syncVolunteers(ctx); err != nil {
			return nil, fmt.Errorf("failed to load the dev volunteer roster for %s: %w", org.ID, err)
		}
	} else {
		syncVolunteers = func(ctx context.Context) error {
			client, err := sheetsclient.NewClientFromServiceAccount(ctx, shared.serviceAccountJSON)
			if err != nil {
				return fmt.Errorf("failed to build sheets client for sync: %w", err)
			}
			roles, err := services.RoleTable(ctx, store)
			if err != nil {
				return err
			}
			fetched, err := client.ListVolunteers(org, roles)
			if err != nil {
				return fmt.Errorf("failed to fetch volunteers for sync: %w", err)
			}
			volunteers.Replace(fetched)
			logger.Info("Volunteer roster synced", zap.Int("count", len(fetched)))
			// The sheet shows display names, and the roster is where they
			// come from. On the startup sync this is the startup publish,
			// which puts right a change made while the server was down or
			// one whose publish failed — and it waits for the roster, since
			// publishing needs one.
			publisher.Trigger()
			return nil
		}

		// The rota sheet is written with the same service account, which must
		// be an editor of it. A publish is of the latest allocated rota as it
		// stands, so each builds its own client rather than keeping one.
		publisher = api.NewSheetPublisher(ctx, func(ctx context.Context) error {
			client, err := sheetsclient.NewWriterFromServiceAccount(ctx, shared.serviceAccountJSON)
			if err != nil {
				return fmt.Errorf("failed to build sheets client for publishing: %w", err)
			}
			_, err = services.PublishRota(ctx, store, client, volunteers, org, logger)
			return err
		}, logger)

		// Populate the roster at startup so reads work before any Organiser syncs. A
		// failure here (transient Sheets outage, say) is not fatal: the server boots
		// with an empty roster and an Organiser can retry via the sync button, matching
		// the store's "degrade to no volunteers" behaviour.
		if err := syncVolunteers(ctx); err != nil {
			logger.Warn("Failed to populate volunteer roster at startup; starting empty", zap.Error(err))
		}
	}

	// A drop-in with no Roles starts and serves, but nobody holds a Role, so
	// nothing can be allocated. That is the state a new Organisation is in until
	// someone creates its Roles, and it is quiet enough to be worth saying out
	// loud.
	if roles, err := services.RoleTable(ctx, store); err != nil {
		logger.Warn("Failed to read roles at startup", zap.Error(err))
	} else if len(roles.ByPriority()) == 0 {
		logger.Warn("No roles exist — the roster will match none and allocation will refuse to run; create them on the Organiser settings screen")
	}

	return api.NewHandler(store, volunteers, org, authenticator, web.Dist(), shared.newMailer, publisher, syncVolunteers, baseLogger), nil
}
