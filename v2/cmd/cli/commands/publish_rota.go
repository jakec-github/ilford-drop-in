package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// PublishRotaCmd creates the publishRota command
func PublishRotaCmd(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "publishRota",
		Short: "Publish the latest rota to the rota sheet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("TODO: Implement publishRota")
			// Service call will go here: services.PublishRota(app.Ctx, app.Cfg, app.SheetsClient, app.Database)
			return nil
		},
	}
}
