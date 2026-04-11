package commands

import (
	"bufio"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// SendAvailabilityRemindersCmd creates the sendAvailabilityReminders command
func SendAvailabilityRemindersCmd(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "sendAvailabilityReminders",
		Short: "Send reminders to volunteers who haven't responded",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reader := bufio.NewReader(os.Stdin)

			deadline, err := promptLine(reader, "\nEnter the deadline for responses (e.g. \"Sunday 27th April\"): ")
			if err != nil {
				return fmt.Errorf("failed to read deadline: %w", err)
			}
			if deadline == "" {
				return fmt.Errorf("deadline cannot be empty")
			}

			fmt.Println()
			fmt.Println("The deadline will appear in emails as:")
			fmt.Printf("  Subject: %q\n", services.ReminderEmailSubject(deadline))
			fmt.Printf("  Body:    \"Deadline for responses is %s when we will create the rota.\"\n", deadline)
			fmt.Println()

			sampleSubject := "[SAMPLE] " + services.ReminderEmailSubject(deadline)
			sampleBody := services.ReminderEmailBody("[Volunteer Name]", "[form URL will appear here]", deadline)

			fmt.Printf("Sending sample email to %s...\n", app.Cfg.GmailUserID)
			if err := app.GmailClient.SendEmail(app.Cfg.GmailUserID, sampleSubject, sampleBody); err != nil {
				return fmt.Errorf("failed to send sample email: %w", err)
			}
			fmt.Println("Sample sent. Check your inbox before continuing.")
			fmt.Println()

			ok, err := promptConfirm(reader, "Send reminders to all volunteers who haven't responded?")
			if err != nil {
				return fmt.Errorf("failed to read confirmation: %w", err)
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
			}

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
