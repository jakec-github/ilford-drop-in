package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// ListVolunteersCmd creates the listVolunteers command
func ListVolunteersCmd(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "listVolunteers",
		Short: "List all volunteers from the volunteer sheet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The roster names the Roles a volunteer holds as strings, so the
			// Roles the app knows have to be read before it can be parsed.
			roles, err := services.RoleTable(app.Ctx, app.Database)
			if err != nil {
				return err
			}

			// Fetch volunteers
			volunteers, err := app.SheetsClient.ListVolunteers(app.Cfg, roles)
			if err != nil {
				return fmt.Errorf("failed to list volunteers: %w", err)
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

			return nil
		},
	}
}
