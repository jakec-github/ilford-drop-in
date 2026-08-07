package commands

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// DefineRotaCmd creates the defineRota command.
//
// It defines the rota the admin screen would propose and nothing else: the
// Sunday after the last rota, at the default hours, asking for the default
// Shape. Everything an admin can change on the define screen (issue #140) is
// changed there — a command line asking for a start date, two times and a Seat
// count per Role would be a worse version of a form, and this command is on its
// way out with the rest of the CLI's rota commands anyway.
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

			proposed, err := services.ProposeRota(app.Ctx, app.Database)
			if err != nil {
				return err
			}
			// Said here rather than left to the validation below, which would
			// name a field this caller was never asked for. Defining from the
			// screen has no equivalent: an admin can fill the boxes in there.
			if proposed.ShiftStartTime == "" || proposed.ShiftEndTime == "" || len(proposed.Shape) == 0 {
				return fmt.Errorf("the drop-in's settings are incomplete - set the shift times and the default shape on the settings screen, or define the rota there")
			}

			shape := make([]services.SeatParams, 0, len(proposed.Shape))
			for _, seat := range proposed.Shape {
				shape = append(shape, services.SeatParams{RoleID: seat.Role.ID, Count: seat.Count})
			}

			result, err := services.DefineRota(app.Ctx, app.Database, app.Logger, services.DefineRotaParams{
				ShiftCount:     shiftCount,
				StartDate:      proposed.StartDate,
				ShiftStartTime: proposed.ShiftStartTime,
				ShiftEndTime:   proposed.ShiftEndTime,
				Shape:          shape,
			})
			if err != nil {
				return err
			}

			// Display results
			fmt.Printf("\n✓ Rotation created successfully!\n\n")
			fmt.Printf("Rotation ID: %s\n", result.Rotation.ID)
			fmt.Printf("Start Date:  %s\n", result.Rotation.Start)
			fmt.Printf("Shift Count: %d\n\n", result.Rotation.ShiftCount)

			fmt.Printf("Shift Dates:\n")
			for i, shift := range result.Shifts {
				fmt.Printf("  %2d. %s\n", i+1, shift.Date)
			}
			fmt.Println()

			return nil
		},
	}
}
