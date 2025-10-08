package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/gmailclient"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/sheetssql"
	"github.com/jakechorley/ilford-drop-in/pkg/utils/logging"
)

// App holds the application dependencies
type App struct {
	cfg          *config.Config
	oauthCfg     *config.OAuthClientConfig
	sheetsClient *sheetsclient.Client
	formsClient  *formsclient.Client
	gmailClient  *gmailclient.Client
	database     *db.DB
	logger       *zap.Logger
	ctx          context.Context
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
			return initApp()
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
	rootCmd.AddCommand(interactiveCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// initApp sets up logger, config, clients, and database
func initApp() error {
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
	app.oauthCfg, err = config.LoadOAuthClientWithEnv(env)
	if err != nil {
		return fmt.Errorf("failed to load OAuth client config: %w", err)
	}
	app.logger.Debug("OAuth configuration loaded successfully")

	// Initialize sheets client
	app.logger.Info("Initializing sheets client")
	app.sheetsClient, err = sheetsclient.NewClient(app.ctx, app.oauthCfg)
	if err != nil {
		return fmt.Errorf("failed to create sheets client: %w", err)
	}
	app.logger.Debug("Sheets client initialized successfully")

	// Initialize forms client (uses same OAuth token from sheets client)
	app.logger.Info("Initializing forms client")
	app.formsClient, err = formsclient.NewClient(app.ctx, app.oauthCfg, app.sheetsClient.Token())
	if err != nil {
		return fmt.Errorf("failed to create forms client: %w", err)
	}
	app.logger.Debug("Forms client initialized successfully")

	// Initialize gmail client (uses same OAuth token from sheets client)
	app.logger.Info("Initializing gmail client")
	app.gmailClient, err = gmailclient.NewClient(app.ctx, app.oauthCfg, app.sheetsClient.Token())
	if err != nil {
		return fmt.Errorf("failed to create gmail client: %w", err)
	}
	app.logger.Debug("Gmail client initialized successfully")

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
	ssqlDB, err := sheetssql.NewDB(app.sheetsClient, app.cfg.DatabaseSheetID, schema)
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

			result, err := services.DefineRota(app.ctx, app.database, app.logger, shiftCount)
			if err != nil {
				return err
			}

			// Display results
			fmt.Printf("\n✓ Rotation created successfully!\n\n")
			fmt.Printf("Rotation ID: %s\n", result.Rotation.ID)
			fmt.Printf("Start Date:  %s\n", result.Rotation.Start)
			fmt.Printf("Shift Count: %d\n\n", result.Rotation.ShiftCount)

			fmt.Printf("Shift Dates:\n")
			for i, shiftDate := range result.ShiftDates {
				fmt.Printf("  %2d. %s\n", i+1, shiftDate.Format("2006-01-02 (Monday)"))
			}
			fmt.Println()

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

			// Call the service
			sentForms, failedEmails, err := services.RequestAvailability(
				app.ctx,
				app.database,
				app.sheetsClient,
				app.formsClient,
				app.gmailClient,
				app.cfg,
				app.logger,
				deadline,
			)
			if err != nil {
				return err
			}

			// Display results
			fmt.Printf("\n✓ Availability request completed!\n\n")

			if len(sentForms) > 0 {
				fmt.Printf("Forms sent to %d volunteers:\n", len(sentForms))
				for _, sf := range sentForms {
					fmt.Printf("  ✓ %s (%s)\n", sf.VolunteerName, sf.Email)
				}
				fmt.Println()
			}

			if len(failedEmails) > 0 {
				fmt.Printf("⚠️  Failed to send %d emails:\n", len(failedEmails))
				for _, fe := range failedEmails {
					fmt.Printf("  ✗ %s (%s): %s\n", fe.VolunteerName, fe.Email, fe.Error)
				}
				fmt.Println()
			}

			if len(sentForms) == 0 && len(failedEmails) == 0 {
				fmt.Println("No new forms to send - all volunteers already have requests.")
			}

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
			volunteers, err := app.sheetsClient.ListVolunteers(app.cfg)
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

func interactiveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "interactive",
		Short: "Start an interactive session (authenticate once, run multiple commands)",
		Long: `Start an interactive session where you can run multiple commands without re-authenticating.
The session will keep running until you type 'exit' or 'quit'.

Type 'help' to see available commands.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("\n🚀 Starting interactive session...")
			fmt.Println("Type 'help' for available commands, 'exit' or 'quit' to leave")

			// Get all sibling commands (excluding interactive itself)
			rootCmd := cmd.Parent()
			commands := make(map[string]*cobra.Command)
			for _, subCmd := range rootCmd.Commands() {
				if subCmd.Name() != "interactive" && subCmd.Name() != "completion" && subCmd.Name() != "help" {
					commands[subCmd.Name()] = subCmd
				}
			}

			scanner := bufio.NewScanner(os.Stdin)

			for {
				fmt.Print("> ")

				if !scanner.Scan() {
					break
				}

				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}

				// Parse command
				parts := strings.Fields(line)
				cmdName := parts[0]
				cmdArgs := parts[1:]

				// Handle exit
				if cmdName == "exit" || cmdName == "quit" {
					fmt.Println("👋 Goodbye!")
					return nil
				}

				// Handle help
				if cmdName == "help" {
					printInteractiveHelp(commands)
					continue
				}

				// Execute command via Cobra
				targetCmd, exists := commands[cmdName]
				if !exists {
					fmt.Printf("❌ Unknown command: %s (type 'help' for available commands)\n\n", cmdName)
					continue
				}

				// Reset command flags and args
				targetCmd.Flags().VisitAll(func(flag *pflag.Flag) {
					flag.Changed = false
					flag.Value.Set(flag.DefValue)
				})

				// Execute the command's RunE directly, bypassing the full Execute() flow
				// This avoids re-running PersistentPreRunE which would call initApp() again
				if err := targetCmd.ParseFlags(cmdArgs); err != nil {
					fmt.Printf("❌ Error parsing flags: %v\n\n", err)
					continue
				}

				// Get non-flag args after parsing flags
				cmdArgs = targetCmd.Flags().Args()

				// Validate args
				if err := targetCmd.Args(targetCmd, cmdArgs); err != nil {
					fmt.Printf("❌ Error: %v\n\n", err)
					continue
				}

				// Execute the RunE function directly
				if targetCmd.RunE != nil {
					if err := targetCmd.RunE(targetCmd, cmdArgs); err != nil {
						fmt.Printf("❌ Error: %v\n\n", err)
					}
				} else if targetCmd.Run != nil {
					targetCmd.Run(targetCmd, cmdArgs)
				}
			}

			if err := scanner.Err(); err != nil {
				return fmt.Errorf("error reading input: %w", err)
			}

			return nil
		},
	}

	return cmd
}

func printInteractiveHelp(commands map[string]*cobra.Command) {
	fmt.Println("\nAvailable commands:")

	// Get command names and sort them
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}

	// Print each command with its short description
	for _, name := range names {
		cmd := commands[name]
		fmt.Printf("  %-30s %s\n", cmd.Use, cmd.Short)
	}

	fmt.Println("\n  help                           Show this help message")
	fmt.Println("  exit, quit                     Exit the interactive session")
}
