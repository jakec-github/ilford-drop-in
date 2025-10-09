package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// GenerateRotaCmd creates the generateRota command
func GenerateRotaCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generateRota",
		Short: "Generate a rota from availability responses",
		RunE: func(cmd *cobra.Command, args []string) error {
			seed, _ := cmd.Flags().GetString("seed")
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			if dryRun {
				fmt.Println("TODO: Implement generateRota (DRY RUN mode)")
			} else {
				fmt.Println("TODO: Implement generateRota")
			}
			if seed != "" {
				fmt.Printf("Using seed: %s\n", seed)
			}
			// Service call will go here: services.GenerateRota(app.Ctx, app.Cfg, app.SheetsClient, app.Database, seed, dryRun)
			return nil
		},
	}

	cmd.Flags().String("seed", "", "Seed for random decisions")
	cmd.Flags().Bool("dry-run", false, "Run without saving to database")

	return cmd
}
