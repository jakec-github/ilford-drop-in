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

			// Display availability matrix for responded groups
			respondedGroups := []services.GroupResponse{}
			notRespondedGroups := []services.GroupResponse{}
			for _, resp := range result.GroupResponses {
				if resp.HasResponded {
					respondedGroups = append(respondedGroups, resp)
				} else {
					notRespondedGroups = append(notRespondedGroups, resp)
				}
			}

			// Display summary by groups
			fmt.Printf("Response Summary (by Group):\n")
			fmt.Printf("  Total Groups:            %d\n", len(result.GroupResponses))
			fmt.Printf("  ✓ Groups Responded:      %d\n", len(respondedGroups))
			fmt.Printf("  ✗ Groups Not Responded:  %d\n", len(notRespondedGroups))
			fmt.Println()

			if len(respondedGroups) > 0 {
				fmt.Printf("Availability Matrix (by Group):\n\n")

				// ANSI color codes
				const (
					colorReset = "\033[0m"
					colorGreen = "\033[32m"
					colorRed   = "\033[31m"
					colorBold  = "\033[1m"
				)

				// Calculate column width for group names
				maxNameLen := 20
				for _, resp := range respondedGroups {
					if len(resp.GroupName) > maxNameLen {
						maxNameLen = len(resp.GroupName)
					}
				}
				nameColWidth := maxNameLen + 2

				// Print header row with dates
				fmt.Printf("%-*s", nameColWidth, "Group")
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

				// Print each group's availability
				for _, resp := range respondedGroups {
					// Create a map of available dates for quick lookup
					availableMap := make(map[string]bool)
					for _, dateStr := range resp.AvailableDates {
						availableMap[dateStr] = true
					}

					// Print group name
					fmt.Printf("%-*s", nameColWidth, resp.GroupName)

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

			// Display groups who haven't responded
			if len(notRespondedGroups) > 0 {
				fmt.Printf("Not Responded (%d):\n", len(notRespondedGroups))
				for _, resp := range notRespondedGroups {
					fmt.Printf("  ✗ %s\n", resp.GroupName)
				}
				fmt.Println()
			}

			return nil
		},
	}
}
