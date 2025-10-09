package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// ListVolunteersCmd creates the listVolunteers command
func ListVolunteersCmd(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "listVolunteers",
		Short: "List all volunteers from the volunteer sheet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fetch volunteers
			volunteers, err := app.SheetsClient.ListVolunteers(app.Cfg)
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
