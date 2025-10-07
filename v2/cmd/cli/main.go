package main

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/sheetssql"
	"github.com/jakechorley/ilford-drop-in/pkg/utils/logging"
)

func main() {
	fmt.Println("Ilford Drop-In CLI")

	// Require environment argument
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: cli <environment>\nExample: cli test")
		os.Exit(1)
	}

	env := os.Args[1]

	// Initialize logger
	logger, err := logging.InitLogger(env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting application", zap.String("environment", env))

	// Load configuration
	logger.Info("Loading configuration")
	cfg, err := config.LoadWithEnv(env)
	if err != nil {
		logger.Fatal("Failed to load config", zap.Error(err))
	}
	logger.Debug("Configuration loaded successfully")

	// Load OAuth client configuration
	logger.Info("Loading OAuth client configuration")
	oauthCfg, err := config.LoadOAuthClientWithEnv(env)
	if err != nil {
		logger.Fatal("Failed to load OAuth client config", zap.Error(err))
	}
	logger.Debug("OAuth configuration loaded successfully")

	// Initialize context
	ctx := context.Background()

	// Initialize sheets client (will trigger OAuth flow if needed)
	logger.Info("Initializing sheets client")
	client, err := sheetsclient.NewClient(ctx, oauthCfg)
	if err != nil {
		logger.Fatal("Failed to create sheets client", zap.Error(err))
	}
	logger.Debug("Sheets client initialized successfully")

	// Initialize database schema
	logger.Info("Initializing database schema")
	schema, err := sheetssql.SchemaFromModels(
		db.Rotation{},
		db.AvailabilityRequest{},
		db.Slot{},
		db.Cover{},
	)
	if err != nil {
		logger.Fatal("Failed to create database schema", zap.Error(err))
	}
	logger.Debug("Database schema created", zap.Int("tables", len(schema.Tables)))

	// Initialize SheetsSQL database
	logger.Info("Connecting to database", zap.String("spreadsheet_id", cfg.DatabaseSheetID))
	ssqlDB, err := sheetssql.NewDB(client, cfg.DatabaseSheetID, schema)
	if err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}

	// Initialize DB layer
	database := db.NewDB(ssqlDB)
	logger.Info("Database initialized successfully")

	// List volunteers
	logger.Info("Fetching volunteers")
	volunteers, err := client.ListVolunteers(cfg)
	if err != nil {
		logger.Fatal("Failed to list volunteers", zap.Error(err))
	}

	logger.Info("Volunteers fetched successfully", zap.Int("count", len(volunteers)))

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

	// Prevent unused variable warning
	_ = database

	logger.Info("Application completed successfully")
}
