package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// ViewResponsesCmd creates the viewResponses command
func ViewResponsesCmd(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "viewResponses [rota_id]",
		Short: "View availability responses (defaults to latest rota)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var rotaID string
			if len(args) > 0 {
				rotaID = args[0]
			}

			app.Logger.Debug("viewResponses command", zap.String("rota_id", rotaID))

			// Call the service
			result, err := services.ViewResponses(
				app.Ctx,
				app.Database,
				app.SheetsClient,
				app.FormsClient,
				app.Cfg,
				app.Logger,
				rotaID,
			)
			if err != nil {
				return err
			}

			// Display results
			fmt.Printf("\n📋 Availability Responses\n\n")
			fmt.Printf("Rota ID:     %s\n", result.RotaID)
			fmt.Printf("Start Date:  %s\n", result.RotaStart)
			fmt.Printf("Shift Count: %d\n\n", result.ShiftCount)

			// Display shift dates
			fmt.Printf("Shift Dates:\n")
			for i, shiftDate := range result.ShiftDates {
				fmt.Printf("  %2d. %s\n", i+1, shiftDate.Format("2006-01-02 (Monday)"))
			}
			fmt.Println()

			// Display summary
			fmt.Printf("Response Summary:\n")
			fmt.Printf("  Total Active Volunteers: %d\n", result.TotalActiveCount)
			fmt.Printf("  ✓ Responded:             %d\n", result.RespondedCount)
			fmt.Printf("  ✗ Not Responded:         %d\n", result.NotRespondedCount)
			fmt.Println()

			// Display availability matrix for responded volunteers
			respondedVolunteers := []services.VolunteerResponse{}
			notRespondedVolunteers := []services.VolunteerResponse{}
			for _, resp := range result.Responses {
				if resp.HasResponded {
					respondedVolunteers = append(respondedVolunteers, resp)
				} else {
					notRespondedVolunteers = append(notRespondedVolunteers, resp)
				}
			}

			if len(respondedVolunteers) > 0 {
				fmt.Printf("Availability Matrix:\n\n")

				// ANSI color codes
				const (
					colorReset = "\033[0m"
					colorGreen = "\033[32m"
					colorRed   = "\033[31m"
					colorBold  = "\033[1m"
				)

				// Calculate column width for volunteer names
				maxNameLen := 20
				for _, resp := range respondedVolunteers {
					if len(resp.VolunteerName) > maxNameLen {
						maxNameLen = len(resp.VolunteerName)
					}
				}
				nameColWidth := maxNameLen + 2

				// Print header row with dates
				fmt.Printf("%-*s", nameColWidth, "Volunteer")
				for _, shiftDate := range result.ShiftDates {
					fmt.Printf("  %-6s", shiftDate.Format("Jan 2"))
				}
				fmt.Println()

				// Print separator
				fmt.Print(strings.Repeat("-", nameColWidth))
				for range result.ShiftDates {
					fmt.Print("  ------")
				}
				fmt.Println()

				// Print each volunteer's availability
				for _, resp := range respondedVolunteers {
					// Create a map of available dates for quick lookup
					availableMap := make(map[string]bool)
					for _, dateStr := range resp.AvailableDates {
						availableMap[dateStr] = true
					}

					// Print volunteer name
					fmt.Printf("%-*s", nameColWidth, resp.VolunteerName)

					// Print availability for each shift date
					for _, shiftDate := range result.ShiftDates {
						dateStr := shiftDate.Format("Mon Jan 2 2006")
						if availableMap[dateStr] {
							fmt.Printf("  %s%-6s%s", colorGreen, "  ✓", colorReset)
						} else {
							fmt.Printf("  %s%-6s%s", colorRed, "  ✗", colorReset)
						}
					}
					fmt.Println()
				}
				fmt.Println()
			}

			// Display volunteers who haven't responded
			if len(notRespondedVolunteers) > 0 {
				fmt.Printf("Not Responded (%d):\n", len(notRespondedVolunteers))
				for _, resp := range notRespondedVolunteers {
					fmt.Printf("  ✗ %s (%s) - %s\n", resp.VolunteerName, resp.Email, resp.Status)
				}
				fmt.Println()
			}

			return nil
		},
	}
}
