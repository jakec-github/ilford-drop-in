package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// PublishRotaCmd creates the publishRota command
func PublishRotaCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publishRota [rotaID]",
		Short: "Publish a rota to Google Sheets",
		Long:  "Publish a rota to Google Sheets. If no rotaID is provided, publishes the latest rota.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get rotaID from args (empty string if not provided)
			rotaID := ""
			if len(args) > 0 {
				rotaID = args[0]
			}

			app.Logger.Debug("publishRota command", zap.String("rota_id", rotaID))

			// Publish the rota to Google Sheets
			publishedRota, err := services.PublishRota(
				app.Ctx,
				app.Database,
				app.SheetsClient,
				app.SheetsClient,
				app.Cfg,
				app.Logger,
				rotaID,
			)
			if err != nil {
				return fmt.Errorf("failed to publish rota: %w", err)
			}

			// Display success message
			fmt.Printf("\n✅ Rota Published Successfully\n\n")
			fmt.Printf("Start Date:  %s\n", publishedRota.StartDate)
			fmt.Printf("Shift Count: %d\n", publishedRota.ShiftCount)
			fmt.Printf("Sheet ID:    %s\n", app.Cfg.RotaSheetID)
			fmt.Println()

			// Display summary table
			fmt.Printf("📅 Published Shifts:\n\n")

			// Print header — a column per capped Role, as the sheet has
			fmt.Printf("%-15s", "Date")
			for _, name := range publishedRota.CappedRoleNames {
				fmt.Printf("  %-20s", name)
			}
			fmt.Printf("  %-40s\n", "Volunteers")

			fmt.Print("---------------")
			for range publishedRota.CappedRoleNames {
				fmt.Print("  --------------------")
			}
			fmt.Println("  ----------------------------------------")

			// Print each shift
			for _, row := range publishedRota.Rows {
				fmt.Printf("%-15s", row.Date)
				for i, name := range publishedRota.CappedRoleNames {
					cell := "—"
					switch {
					case row.Closed && i == 0:
						// Announced once, in the first column, as in the sheet
						cell = "CLOSED"
					case row.Closed:
					case len(row.CappedRoles[name]) > 0:
						cell = strings.Join(row.CappedRoles[name], ", ")
					}
					fmt.Printf("  %-20s", cell)
				}

				volunteers := "—"
				if len(row.Volunteers) > 0 {
					volunteers = fmt.Sprintf("%d volunteers", len(row.Volunteers))
				}
				fmt.Printf("  %-40s\n", volunteers)
			}

			fmt.Println()
			fmt.Println("✅ Rota has been published to Google Sheets.")

			return nil
		},
	}

	return cmd
}
