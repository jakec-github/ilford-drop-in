package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsClient"
)

func main() {
	// Load configuration
	fmt.Println("Loading configuration...")
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Load OAuth client configuration
	fmt.Println("Loading OAuth client configuration...")
	oauthCfg, err := config.LoadOAuthClient()
	if err != nil {
		log.Fatalf("Failed to load OAuth client config: %v", err)
	}

	// Initialize sheets client (will trigger OAuth flow if needed)
	fmt.Println("Initialising sheets client...")
	ctx := context.Background()
	client, err := sheetsClient.NewClient(ctx, oauthCfg)
	if err != nil {
		log.Fatalf("Failed to create sheets client: %v", err)
	}

	// List volunteers
	fmt.Println("\nFetching volunteers...")
	volunteers, err := client.ListVolunteers(cfg)
	if err != nil {
		log.Fatalf("Failed to list volunteers: %v", err)
	}

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

	os.Exit(0)
}
