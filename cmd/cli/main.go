package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/cmd/cli/commands"
	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/utils/logging"
)

var (
	env string
	app *commands.AppContext
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cli",
		Short: "Ilford Drop-In CLI - Manage volunteer rotas",
		Long: `A CLI tool for defining, allocating and publishing volunteer rotas.

Availability is collected and chased through the web app, not here.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip initialization for help commands - no need for OAuth/API clients or env flag
			helpFlag, _ := cmd.Flags().GetBool("help")
			if helpFlag || cmd.Name() == "help" {
				return nil
			}
			// Validate env flag (required for non-help commands)
			if env == "" {
				return fmt.Errorf("required flag \"env\" not set")
			}
			return initApp()
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if app != nil && app.Logger != nil {
				app.Logger.Sync()
			}
		},
	}

	// Add persistent environment flag (validated in PersistentPreRunE, not here, so help works without it)
	rootCmd.PersistentFlags().StringVarP(&env, "env", "e", "", "Environment (required: test, prod, etc.)")

	// Add commands with lazy initialization
	// These will use the app context after it's initialized by PersistentPreRunE
	rootCmd.AddCommand(newLazyCommand(commands.DefineRotaCmd))
	rootCmd.AddCommand(newLazyCommand(commands.AllocateRotaCmd))
	rootCmd.AddCommand(newLazyCommand(commands.PublishRotaCmd))
	rootCmd.AddCommand(newLazyCommand(commands.ChangeRotaCmd))
	rootCmd.AddCommand(newLazyCommand(commands.ListVolunteersCmd))
	rootCmd.AddCommand(newLazyCommand(commands.ViewHistoricalResponsesCmd))
	// One-off, deleted in the next commit once every environment has run it.
	rootCmd.AddCommand(newLazyCommand(commands.BackfillShiftClosedCmd))

	// Not lazy, and deliberately not initialised: validate-config reads a file
	// and nothing else, so it can vet a prod config from a laptop. It shadows
	// PersistentPreRunE itself — see the command.
	rootCmd.AddCommand(commands.ValidateConfigCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// newLazyCommand creates a wrapper command that lazily delegates to the actual command
// after the app context has been initialized
func newLazyCommand(cmdFunc func(*commands.AppContext) *cobra.Command) *cobra.Command {
	// Create a temporary command just to get metadata
	tempCmd := cmdFunc(&commands.AppContext{})

	wrapper := &cobra.Command{
		Use:   tempCmd.Use,
		Short: tempCmd.Short,
		Long:  tempCmd.Long,
		Args:  tempCmd.Args,
	}

	// Copy flags from the template
	wrapper.Flags().AddFlagSet(tempCmd.Flags())

	// Override RunE to use the actual command with the initialized app context
	wrapper.RunE = func(cmd *cobra.Command, args []string) error {
		if app == nil {
			return fmt.Errorf("application not initialized")
		}
		// Get the actual command with the real app context
		actualCmd := cmdFunc(app)
		// Execute it
		return actualCmd.RunE(cmd, args)
	}

	return wrapper
}

// initApp sets up logger, config, clients, and database
func initApp() error {
	var err error
	logger, err := logging.InitLogger(env)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	logger.Info("Starting application", zap.String("environment", env))

	// Load configuration
	logger.Debug("Loading configuration")
	cfg, err := config.LoadWithEnv(env)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	logger.Debug("Configuration loaded successfully")

	// Load OAuth client configuration
	logger.Debug("Loading OAuth client configuration")
	oauthCfg, err := config.LoadOAuthClientWithEnv(env)
	if err != nil {
		return fmt.Errorf("failed to load OAuth client config: %w", err)
	}
	logger.Debug("OAuth configuration loaded successfully")

	ctx := context.Background()

	// Initialize sheets client
	logger.Debug("Initializing sheets client")
	sheetsClient, err := sheetsclient.NewClient(ctx, oauthCfg, env)
	if err != nil {
		return fmt.Errorf("failed to create sheets client: %w", err)
	}
	logger.Debug("Sheets client initialized successfully")

	// Get authenticated user email for audit trail (from token, no extra scopes needed)
	userEmail, err := utils.GetTokenEmail(ctx, sheetsClient.Token())
	if err != nil {
		return fmt.Errorf("failed to get user email: %w", err)
	}
	logger.Debug("Resolved user email", zap.String("email", userEmail))

	// Initialize database
	logger.Debug("Connecting to PostgreSQL database")
	database, err := db.NewDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	logger.Debug("Running database migrations")
	if err := database.RunMigrations(ctx); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	logger.Debug("PostgreSQL database initialized successfully")

	// Initialize the global app context
	app = &commands.AppContext{
		Cfg:          cfg,
		SheetsClient: sheetsClient,
		Database:     database,
		Logger:       logger,
		Ctx:          ctx,
		UserEmail:    userEmail,
	}

	return nil
}
