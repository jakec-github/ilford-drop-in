package commands

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
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

			app.Logger.Debug("viewHistoricalResponses command", zap.Int("count", count))

			result, err := services.ViewHistoricalResponses(
				app.Ctx,
				app.Database,
				app.SheetsClient,
				app.FormsClient,
				app.Cfg,
				app.Logger,
				count,
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
						// Green if availability is more than half the shifts
						if status.AvailableCount > status.ShiftCount/2 {
							fmt.Printf("%s%-*s%s", colorGreen, rotaColWidth, cell, colorReset)
						} else {
							fmt.Printf("%-*s", rotaColWidth, cell)
						}
					case "no_availability":
						cell := fmt.Sprintf("0/%d", status.ShiftCount)
						fmt.Printf("%s%-*s%s", colorRed, rotaColWidth, cell, colorReset)
					case "no_response":
						fmt.Printf("%s%-*s%s", colorRed, rotaColWidth, "No response", colorReset)
					case "no_form":
						fmt.Printf("%s%-*s%s", colorDim, rotaColWidth, "No form", colorReset)
					case "form_error":
						fmt.Printf("%s%-*s%s", colorYellow, rotaColWidth, "Error", colorReset)
					}
				}
				fmt.Println()
			}

			// Legend
			fmt.Println()
			fmt.Println("Legend:")
			fmt.Printf("  %sX/Y%s   = available for X of Y shifts\n", colorGreen, colorReset)
			fmt.Printf("  %s0/Y%s   = responded with no availability\n", colorRed, colorReset)
			fmt.Printf("  %sNo response%s = form sent, no response before cut-off\n", colorRed, colorReset)
			fmt.Printf("  %sNo form%s     = no form was sent\n", colorDim, colorReset)
			fmt.Printf("  %sError%s       = form could not be accessed\n", colorYellow, colorReset)

			return nil
		},
	}

	return cmd
}
