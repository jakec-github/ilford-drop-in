package commands

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// ViewHistoricalResponsesCmd creates the viewHistoricalResponses command
func ViewHistoricalResponsesCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "viewHistoricalResponses <count>",
		Short: "View historical volunteer response status across recent rotations",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			count, err := strconv.Atoi(args[0])
			if err != nil || count < 1 {
				return fmt.Errorf("count must be a positive integer, got: %s", args[0])
			}

			volunteerIDs, _ := cmd.Flags().GetStringSlice("volunteers")

			app.Logger.Debug("viewHistoricalResponses command",
				zap.Int("count", count),
				zap.Strings("volunteer_ids", volunteerIDs))

			result, err := services.ViewHistoricalResponses(
				app.Ctx,
				app.Database,
				app.SheetsClient,
				app.FormsClient,
				app.Cfg,
				app.Logger,
				count,
				volunteerIDs,
			)
			if err != nil {
				return err
			}

			// ANSI color codes
			const (
				colorReset  = "\033[0m"
				colorGreen  = "\033[32m"
				colorRed    = "\033[31m"
				colorYellow = "\033[33m"
				colorOrange = "\033[38;5;208m"
				colorDim    = "\033[2m"
			)

			fmt.Printf("\nHistorical Availability Responses (last %d rotations)\n\n", len(result.Rotations))

			// Calculate column widths
			maxNameLen := 20
			for _, vol := range result.Volunteers {
				if len(vol.DisplayName) > maxNameLen {
					maxNameLen = len(vol.DisplayName)
				}
			}
			nameColWidth := maxNameLen + 2
			rotaColWidth := 14

			// Print header row with rotation labels
			fmt.Printf("%-*s", nameColWidth, "")
			for i := range result.Rotations {
				fmt.Printf("%-*s", rotaColWidth, fmt.Sprintf("Rota %d", i+1))
			}
			fmt.Println()

			// Print header row with start dates
			fmt.Printf("%-*s", nameColWidth, "")
			for _, rota := range result.Rotations {
				startDate, _ := time.Parse("2006-01-02", rota.Start)
				fmt.Printf("%-*s", rotaColWidth, startDate.Format("Jan 02"))
			}
			fmt.Println()

			// Print separator
			fmt.Print(strings.Repeat("-", nameColWidth))
			for range result.Rotations {
				fmt.Print(strings.Repeat("-", rotaColWidth))
			}
			fmt.Println()

			// Sort volunteers by total availability (lowest first)
			sort.Slice(result.Volunteers, func(i, j int) bool {
				return totalAvailability(result.Volunteers[i], result.Rotations, result.Matrix) <
					totalAvailability(result.Volunteers[j], result.Rotations, result.Matrix)
			})

			// Print each volunteer's row
			for _, vol := range result.Volunteers {
				fmt.Printf("%-*s", nameColWidth, vol.DisplayName)

				for _, rota := range result.Rotations {
					status, exists := result.Matrix[vol.ID][rota.ID]
					if !exists {
						fmt.Printf("%s%-*s%s", colorDim, rotaColWidth, "No form", colorReset)
						continue
					}

					switch status.Status {
					case "available":
						cell := fmt.Sprintf("%d/%d", status.AvailableCount, status.ShiftCount)
						color := availabilityColor(status.AvailableCount, status.ShiftCount, colorGreen, colorYellow, colorOrange)
						fmt.Printf("%s%-*s%s", color, rotaColWidth, cell, colorReset)
					case "no_availability":
						cell := fmt.Sprintf("0/%d", status.ShiftCount)
						fmt.Printf("%s%-*s%s", colorRed, rotaColWidth, cell, colorReset)
					case "no_response":
						fmt.Printf("%s%-*s%s", colorRed, rotaColWidth, "No response", colorReset)
					case "no_form":
						fmt.Printf("%s%-*s%s", colorDim, rotaColWidth, "No form", colorReset)
					case "form_error":
						fmt.Printf("%s%-*s%s", colorDim, rotaColWidth, "Error", colorReset)
					}
				}
				fmt.Println()
			}

			// Legend
			fmt.Println()
			fmt.Println("Legend:")
			fmt.Printf("  %sX/Y%s   = available for more than half of shifts\n", colorGreen, colorReset)
			fmt.Printf("  %sX/Y%s   = available for half or fewer shifts\n", colorYellow, colorReset)
			fmt.Printf("  %sX/Y%s   = available for 3 or fewer shifts\n", colorOrange, colorReset)
			fmt.Printf("  %s0/Y%s   = responded with no availability\n", colorRed, colorReset)
			fmt.Printf("  %sNo response%s = form sent, no response before cut-off\n", colorRed, colorReset)
			fmt.Printf("  %sNo form%s     = no form was sent\n", colorDim, colorReset)
			fmt.Printf("  %sError%s       = form could not be accessed\n", colorDim, colorReset)

			return nil
		},
	}

	cmd.Flags().StringSlice("volunteers", nil, "Comma-separated list of volunteer IDs to filter by")

	return cmd
}

// totalAvailability sums a volunteer's available shift count across all rotations.
// Non-"available" statuses (no_form, no_response, no_availability, form_error) count as 0.
func totalAvailability(vol model.Volunteer, rotations []db.Rotation, matrix map[string]map[string]services.VolunteerRotaStatus) int {
	total := 0
	for _, rota := range rotations {
		if status, ok := matrix[vol.ID][rota.ID]; ok && status.Status == "available" {
			total += status.AvailableCount
		}
	}
	return total
}

// availabilityColor returns the ANSI color for an availability count.
//   - > half of shifts: green
//   - <= half but > 3: yellow
//   - <= 3: orange
func availabilityColor(available, total int, green, yellow, orange string) string {
	if available <= 3 {
		return orange
	}
	if available <= total/2 {
		return yellow
	}
	return green
}
