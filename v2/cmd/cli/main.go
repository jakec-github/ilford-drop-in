package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/sheetssql"
	"github.com/jakechorley/ilford-drop-in/pkg/utils/logging"
)

// App holds the application dependencies
type App struct {
	cfg      *config.Config
	client   *sheetsclient.Client
	database *db.DB
	logger   *zap.Logger
	ctx      context.Context
}

var (
	env string
	app *App
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cli",
		Short: "Ilford Drop-In CLI - Manage volunteer rotas",
		Long:  `A CLI tool for managing volunteer rotas, availability requests, and shift scheduling.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return initializeApp()
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if app != nil && app.logger != nil {
				app.logger.Sync()
			}
		},
	}

	// Add persistent environment flag
	rootCmd.PersistentFlags().StringVarP(&env, "env", "e", "", "Environment (required: test, prod, etc.)")
	rootCmd.MarkPersistentFlagRequired("env")

	// Add all commands
	rootCmd.AddCommand(defineRotaCmd())
	rootCmd.AddCommand(requestAvailabilityCmd())
	rootCmd.AddCommand(sendAvailabilityRemindersCmd())
	rootCmd.AddCommand(viewResponsesCmd())
	rootCmd.AddCommand(generateRotaCmd())
	rootCmd.AddCommand(publishRotaCmd())
	rootCmd.AddCommand(addCoverCmd())
	rootCmd.AddCommand(listVolunteersCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// initializeApp sets up logger, config, clients, and database
func initializeApp() error {
	var err error
	app = &App{
		ctx: context.Background(),
	}

	// Initialize logger
	app.logger, err = logging.InitLogger(env)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	app.logger.Info("Starting application", zap.String("environment", env))

	// Load configuration
	app.logger.Info("Loading configuration")
	app.cfg, err = config.LoadWithEnv(env)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	app.logger.Debug("Configuration loaded successfully")

	// Load OAuth client configuration
	app.logger.Info("Loading OAuth client configuration")
	oauthCfg, err := config.LoadOAuthClientWithEnv(env)
	if err != nil {
		return fmt.Errorf("failed to load OAuth client config: %w", err)
	}
	app.logger.Debug("OAuth configuration loaded successfully")

	// Initialize sheets client
	app.logger.Info("Initializing sheets client")
	app.client, err = sheetsclient.NewClient(app.ctx, oauthCfg)
	if err != nil {
		return fmt.Errorf("failed to create sheets client: %w", err)
	}
	app.logger.Debug("Sheets client initialized successfully")

	// Initialize database schema
	app.logger.Info("Initializing database schema")
	schema, err := sheetssql.SchemaFromModels(
		db.Rotation{},
		db.AvailabilityRequest{},
		db.Slot{},
		db.Cover{},
	)
	if err != nil {
		return fmt.Errorf("failed to create database schema: %w", err)
	}
	app.logger.Debug("Database schema created", zap.Int("tables", len(schema.Tables)))

	// Initialize SheetsSQL database
	app.logger.Info("Connecting to database", zap.String("spreadsheet_id", app.cfg.DatabaseSheetID))
	ssqlDB, err := sheetssql.NewDB(app.client, app.cfg.DatabaseSheetID, schema)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	// Initialize DB layer
	app.database = db.NewDB(ssqlDB)
	app.logger.Info("Database initialized successfully")

	return nil
}

// Command definitions

func defineRotaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "defineRota <shift_count>",
		Short: "Define a new rota with the specified number of shifts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shiftCount, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("shift_count must be a number: %w", err)
			}

			app.logger.Info("defineRota command", zap.Int("shift_count", shiftCount))
			fmt.Printf("TODO: Implement defineRota with %d shifts\n", shiftCount)
			// Service call will go here: services.DefineRota(app.ctx, app.database, shiftCount)
			return nil
		},
	}
}

func requestAvailabilityCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "requestAvailability <deadline>",
		Short: "Request availability from volunteers with the given deadline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deadline := args[0]

			app.logger.Info("requestAvailability command", zap.String("deadline", deadline))
			fmt.Printf("TODO: Implement requestAvailability with deadline %s\n", deadline)
			// Service call will go here: services.RequestAvailability(app.ctx, app.cfg, app.client, app.database, deadline)
			return nil
		},
	}
}

func sendAvailabilityRemindersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sendAvailabilityReminders <deadline>",
		Short: "Send reminders to volunteers who haven't responded",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deadline := args[0]

			app.logger.Info("sendAvailabilityReminders command", zap.String("deadline", deadline))
			fmt.Printf("TODO: Implement sendAvailabilityReminders with deadline %s\n", deadline)
			// Service call will go here: services.SendAvailabilityReminders(app.ctx, app.cfg, app.client, app.database, deadline)
			return nil
		},
	}
}

func viewResponsesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "viewResponses [rota_id]",
		Short: "View availability responses (defaults to latest rota)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var rotaID string
			if len(args) > 0 {
				rotaID = args[0]
			}

			if rotaID != "" {
				app.logger.Info("viewResponses command", zap.String("rota_id", rotaID))
				fmt.Printf("TODO: Implement viewResponses for rota %s\n", rotaID)
			} else {
				app.logger.Info("viewResponses command (latest rota)")
				fmt.Println("TODO: Implement viewResponses for latest rota")
			}
			// Service call will go here: services.ViewResponses(app.ctx, app.cfg, app.client, app.database, rotaID)
			return nil
		},
	}
}

func generateRotaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generateRota",
		Short: "Generate a rota from availability responses",
		RunE: func(cmd *cobra.Command, args []string) error {
			seed, _ := cmd.Flags().GetString("seed")
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			app.logger.Info("generateRota command",
				zap.String("seed", seed),
				zap.Bool("dry_run", dryRun))

			if dryRun {
				fmt.Println("TODO: Implement generateRota (DRY RUN mode)")
			} else {
				fmt.Println("TODO: Implement generateRota")
			}
			if seed != "" {
				fmt.Printf("Using seed: %s\n", seed)
			}
			// Service call will go here: services.GenerateRota(app.ctx, app.cfg, app.client, app.database, seed, dryRun)
			return nil
		},
	}

	cmd.Flags().String("seed", "", "Seed for random decisions")
	cmd.Flags().Bool("dry-run", false, "Run without saving to database")

	return cmd
}

func publishRotaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "publishRota",
		Short: "Publish the latest rota to the rota sheet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app.logger.Info("publishRota command")
			fmt.Println("TODO: Implement publishRota")
			// Service call will go here: services.PublishRota(app.ctx, app.cfg, app.client, app.database)
			return nil
		},
	}
}

func addCoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "addCover <shift_date> <covered_volunteer_id> <covering_volunteer_id> [rota_id]",
		Short: "Add a cover/swap for a shift",
		Args:  cobra.RangeArgs(3, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			shiftDate := args[0]
			coveredVolunteerID := args[1]
			coveringVolunteerID := args[2]
			var rotaID string
			if len(args) > 3 {
				rotaID = args[3]
			}

			app.logger.Info("addCover command",
				zap.String("shift_date", shiftDate),
				zap.String("covered_volunteer_id", coveredVolunteerID),
				zap.String("covering_volunteer_id", coveringVolunteerID),
				zap.String("rota_id", rotaID))

			fmt.Printf("TODO: Implement addCover for shift %s\n", shiftDate)
			// Service call will go here: services.AddCover(app.ctx, app.cfg, app.client, app.database, shiftDate, coveredVolunteerID, coveringVolunteerID, rotaID)
			return nil
		},
	}
}

func listVolunteersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "listVolunteers",
		Short: "List all volunteers from the volunteer sheet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			app.logger.Info("listVolunteers command")

			// Fetch volunteers
			volunteers, err := app.client.ListVolunteers(app.cfg)
			if err != nil {
				return fmt.Errorf("failed to list volunteers: %w", err)
			}

			app.logger.Info("Volunteers fetched successfully", zap.Int("count", len(volunteers)))

			// Print volunteers
			fmt.Printf("\nFound %d volunteers:\n\n", len(volunteers))
			for _, v := range volunteers {
				groupInfo := ""
				if v.GroupKey != "" {
					groupInfo = fmt.Sprintf(" [Group: %s]", v.GroupKey)
				}
				fmt.Printf("- %s %s (%s) - %s - %s%s\n",
					v.FirstName,
					v.LastName,
					v.ID,
					v.Status,
					v.Email,
					groupInfo,
				)
			}

			return nil
		},
	}
}
