package commands

import (
	"bufio"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// RequestAvailabilityCmd creates the requestAvailability command
func RequestAvailabilityCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "requestAvailability",
		Short: "Request availability from volunteers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			noEmail, _ := cmd.Flags().GetBool("no-email")

			app.Logger.Debug("requestAvailability command", zap.Bool("no_email", noEmail))

			deadline := ""

			if !noEmail {
				reader := bufio.NewReader(os.Stdin)

				var err error
				deadline, err = promptLine(reader, "\nEnter the deadline for responses (e.g. \"Sunday 27th April\"): ")
				if err != nil {
					return fmt.Errorf("failed to read deadline: %w", err)
				}
				if deadline == "" {
					return fmt.Errorf("deadline cannot be empty")
				}

				fmt.Println()
				fmt.Println("The deadline will appear in emails as:")
				fmt.Printf("  Subject: %q\n", services.AvailabilityEmailSubject(deadline))
				fmt.Printf("  Body:    \"Deadline for responses is %s when we will create the rota.\"\n", deadline)
				fmt.Println()

				sampleSubject := "[SAMPLE] " + services.AvailabilityEmailSubject(deadline)
				sampleBody := services.AvailabilityEmailBody("[Volunteer Name]", "[form URL will appear here]", deadline)

				fmt.Printf("Sending sample email to %s...\n", app.Cfg.GmailUserID)
				if err := app.GmailClient.SendEmail(app.Cfg.GmailUserID, sampleSubject, sampleBody); err != nil {
					return fmt.Errorf("failed to send sample email: %w", err)
				}
				fmt.Println("Sample sent. Check your inbox before continuing.")
				fmt.Println()

				ok, err := promptConfirm(reader, "Send availability requests to all volunteers?")
				if err != nil {
					return fmt.Errorf("failed to read confirmation: %w", err)
				}
				if !ok {
					fmt.Println("Aborted.")
					return nil
				}
			}

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
