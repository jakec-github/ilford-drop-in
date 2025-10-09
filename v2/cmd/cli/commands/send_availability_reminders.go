package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// SendAvailabilityRemindersCmd creates the sendAvailabilityReminders command
func SendAvailabilityRemindersCmd(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "sendAvailabilityReminders <deadline>",
		Short: "Send reminders to volunteers who haven't responded",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deadline := args[0]

			// Call the service
			remindersSent, failedEmails, err := services.SendAvailabilityReminders(
				app.Ctx,
				app.Database,
				app.SheetsClient,
				app.FormsClient,
				app.GmailClient,
				app.Cfg,
				app.Logger,
				deadline,
			)
			if err != nil {
				return err
			}

			// Display results
			fmt.Printf("\n✓ Availability reminders completed!\n\n")

			if len(remindersSent) > 0 {
				fmt.Printf("Reminders sent to %d volunteers:\n", len(remindersSent))
				for _, rs := range remindersSent {
					fmt.Printf("  ✓ %s (%s)\n", rs.VolunteerName, rs.Email)
				}
				fmt.Println()
			}

			if len(failedEmails) > 0 {
				fmt.Printf("⚠️  Failed to send %d reminder emails:\n", len(failedEmails))
				for _, fe := range failedEmails {
					fmt.Printf("  ✗ %s (%s): %s\n", fe.VolunteerName, fe.Email, fe.Error)
				}
				fmt.Println()
			}

			if len(remindersSent) == 0 && len(failedEmails) == 0 {
				fmt.Println("No reminders needed - all volunteers have responded or no requests have been sent.")
			}

			return nil
		},
	}
}
