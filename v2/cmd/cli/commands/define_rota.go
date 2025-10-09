package commands

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// DefineRotaCmd creates the defineRota command
func DefineRotaCmd(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "defineRota <shift_count>",
		Short: "Define a new rota with the specified number of shifts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shiftCount, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("shift_count must be a number: %w", err)
			}

			result, err := services.DefineRota(app.Ctx, app.Database, app.Logger, shiftCount)
			if err != nil {
				return err
			}

			// Display results
			fmt.Printf("\n✓ Rotation created successfully!\n\n")
			fmt.Printf("Rotation ID: %s\n", result.Rotation.ID)
			fmt.Printf("Start Date:  %s\n", result.Rotation.Start)
			fmt.Printf("Shift Count: %d\n\n", result.Rotation.ShiftCount)

			fmt.Printf("Shift Dates:\n")
			for i, shiftDate := range result.ShiftDates {
				fmt.Printf("  %2d. %s\n", i+1, shiftDate.Format("2006-01-02 (Monday)"))
			}
			fmt.Println()

			return nil
		},
	}
}
