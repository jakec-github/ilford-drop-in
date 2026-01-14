package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// RequestAvailabilityCmd creates the requestAvailability command
func RequestAvailabilityCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "requestAvailability <deadline>",
		Short: "Request availability from volunteers with the given deadline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deadline := args[0]
			noEmail, _ := cmd.Flags().GetBool("no-email")

			app.Logger.Debug("requestAvailability command", zap.Bool("no_email", noEmail))

			// Call the service
			sentForms, failedEmails, err := services.RequestAvailability(
				app.Ctx,
				app.Database,
				app.SheetsClient,
				app.FormsClient,
				app.GmailClient,
				app.Cfg,
				app.Logger,
				deadline,
				noEmail,
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

	cmd.Flags().Bool("no-email", false, "Create forms only, don't send emails or mark as sent")

	return cmd
}
