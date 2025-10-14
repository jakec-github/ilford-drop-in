package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/cmd/cli/commands"
	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/gmailclient"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/sheetssql"
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
		Long:  `A CLI tool for managing volunteer rotas, availability requests, and shift scheduling.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initApp()
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if app != nil && app.Logger != nil {
				app.Logger.Sync()
			}
		},
	}

	// Add persistent environment flag
	rootCmd.PersistentFlags().StringVarP(&env, "env", "e", "", "Environment (required: test, prod, etc.)")
	rootCmd.MarkPersistentFlagRequired("env")

	// Add commands with lazy initialization
	// These will use the app context after it's initialized by PersistentPreRunE
	rootCmd.AddCommand(newLazyCommand(commands.DefineRotaCmd))
	rootCmd.AddCommand(newLazyCommand(commands.RequestAvailabilityCmd))
	rootCmd.AddCommand(newLazyCommand(commands.SendAvailabilityRemindersCmd))
	rootCmd.AddCommand(newLazyCommand(commands.ViewResponsesCmd))
	rootCmd.AddCommand(newLazyCommand(commands.GenerateRotaCmd))
	rootCmd.AddCommand(newLazyCommand(commands.PublishRotaCmd))
	rootCmd.AddCommand(newLazyCommand(commands.AddCoverCmd))
	rootCmd.AddCommand(newLazyCommand(commands.ListVolunteersCmd))
	rootCmd.AddCommand(newLazyCommand(commands.InteractiveCmd))

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
	sheetsClient, err := sheetsclient.NewClient(ctx, oauthCfg)
	if err != nil {
		return fmt.Errorf("failed to create sheets client: %w", err)
	}
	logger.Debug("Sheets client initialized successfully")

	// Initialize forms client (uses same OAuth token from sheets client)
	logger.Debug("Initializing forms client")
	formsClient, err := formsclient.NewClient(ctx, oauthCfg, sheetsClient.Token())
	if err != nil {
		return fmt.Errorf("failed to create forms client: %w", err)
	}
	logger.Debug("Forms client initialized successfully")

	// Initialize gmail client (uses same OAuth token from sheets client)
	logger.Debug("Initializing gmail client")
	gmailClient, err := gmailclient.NewClient(ctx, oauthCfg, sheetsClient.Token())
	if err != nil {
		return fmt.Errorf("failed to create gmail client: %w", err)
	}
	logger.Debug("Gmail client initialized successfully")

	// Initialize database schema
	logger.Debug("Initializing database schema")
	schema, err := sheetssql.SchemaFromModels(
		db.Rotation{},
		db.AvailabilityRequest{},
		db.Allocation{},
		db.Cover{},
	)
	if err != nil {
		return fmt.Errorf("failed to create database schema: %w", err)
	}
	logger.Debug("Database schema created", zap.Int("tables", len(schema.Tables)))

	// Initialize SheetsSQL database
	logger.Debug("Connecting to database", zap.String("spreadsheet_id", cfg.DatabaseSheetID))
	ssqlDB, err := sheetssql.NewDB(sheetsClient, cfg.DatabaseSheetID, schema)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	// Initialize DB layer
	database := db.NewDB(ssqlDB)
	logger.Debug("Database initialized successfully")

	// Initialize the global app context
	app = &commands.AppContext{
		Cfg:          cfg,
		SheetsClient: sheetsClient,
		FormsClient:  formsClient,
		GmailClient:  gmailClient,
		Database:     database,
		Logger:       logger,
		Ctx:          ctx,
	}

	return nil
}
