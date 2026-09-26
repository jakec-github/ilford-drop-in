package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// PublishRotaCmd creates the publishRota command
func PublishRotaCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publishRota",
		Short: "Publish the latest allocated rota to Google Sheets",
		Long: "Bring the rota sheet's Latest tab up to date with the rota allocated most recently. " +
			"The server does this itself after every change; this is the same publish, run by hand.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sheet, err := services.PublishRota(
				app.Ctx,
				app.Database,
				app.SheetsClient,
				app.SheetsClient,
				app.Cfg,
				app.Logger,
			)
			if err != nil {
				return fmt.Errorf("failed to publish rota: %w", err)
			}
			if sheet == nil {
				fmt.Println("No rota has been allocated yet, so there is nothing to publish.")
				return nil
			}

			fmt.Printf("\n✅ Rota Published Successfully\n\n")
			fmt.Printf("Sheet ID: %s\n\n", app.Cfg.RotaSheetID)
			for _, row := range sheet.OwnedValues() {
				for _, cell := range row {
					if cell == "" {
						cell = "—"
					}
					fmt.Printf("%-20s", cell)
				}
				fmt.Println()
			}
			fmt.Println()

			return nil
		},
	}

	return cmd
}
