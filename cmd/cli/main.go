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
		Long: `A CLI tool for the Google Sheets around the Ilford Drop-in rota.

The rota itself lives in the web app: defining it, preparing its shifts, asking
volunteers, allocating it and changing it afterwards all happen there. What is
left here reads or writes a Sheet.`,
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
	// There is no defineRota, allocateRota or changeRota command. The whole life
	// of a rota is in the app: it is defined on Admin → Allocation, allocated
	// from the draft on the same screen, and altered a person at a time on the
	// rota page (issues #140, #144, #145; #64 for the editor).
	//
	// Allocating is the one that could never come back. It re-solves and commits
	// only the rota the admin was shown, and a command that solved and committed
	// in one step could not honour that — two paths where one breaks the rule is
	// worse than one path (ADR 0008).
	rootCmd.AddCommand(newLazyCommand(commands.PublishRotaCmd))
	rootCmd.AddCommand(newLazyCommand(commands.ListVolunteersCmd))
	rootCmd.AddCommand(newLazyCommand(commands.ViewHistoricalResponsesCmd))

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
