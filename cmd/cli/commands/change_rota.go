package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// ChangeRotaCmd creates the changeRota command
func ChangeRotaCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "changeRota <date>",
		Short: "Record a cover or change for a published rota shift",
		Long: `Record a cover or change for a published rota shift.
Creates an audit trail (cover) and alterations that modify the effective rota.

Examples:
  # Replace one volunteer with another
  cli -e prod changeRota 2025-03-02 --out vol-1 --in vol-2 --reason "Holiday cover"

  # Swap two volunteers between dates
  cli -e prod changeRota 2025-03-02 --out vol-1 --in vol-2 --swap-date 2025-03-09 --reason "Swap"

  # Add a custom entry
  cli -e prod changeRota 2025-03-02 --in-custom "External John" --reason "Extra help"

  # Remove a volunteer
  cli -e prod changeRota 2025-03-02 --out vol-1 --reason "No longer available"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			date := args[0]

			// Read flags from cmd (the wrapper command that parsed them)
			// rather than local var bindings, due to the lazy command pattern
			inVol, _ := cmd.Flags().GetString("in")
			outVol, _ := cmd.Flags().GetString("out")
			inCustom, _ := cmd.Flags().GetString("in-custom")
			outCustom, _ := cmd.Flags().GetString("out-custom")
			swapDate, _ := cmd.Flags().GetString("swap-date")
			reason, _ := cmd.Flags().GetString("reason")

			params := services.ChangeRotaParams{
				Date:      date,
				In:        inVol,
				Out:       outVol,
				InCustom:  inCustom,
				OutCustom: outCustom,
				SwapDate:  swapDate,
				Reason:    reason,
				UserEmail: app.UserEmail,
			}

			result, err := services.ChangeRota(app.Ctx, app.Database, params, app.Logger)
			if err != nil {
				return err
			}

			fmt.Printf("Cover recorded: %s\n", result.CoverID)
			fmt.Printf("Alterations created: %d\n", len(result.Alterations))
			for _, alt := range result.Alterations {
				vol := alt.VolunteerID
				if vol == "" {
					vol = fmt.Sprintf("[%s]", alt.CustomValue)
				}
				fmt.Printf("  %s %s on %s\n", alt.Direction, vol, alt.ShiftDate)
			}

			app.Logger.Info("Rota change completed",
				zap.String("cover_id", result.CoverID),
				zap.Int("alterations", len(result.Alterations)))

			return nil
		},
	}

	cmd.Flags().String("in", "", "Volunteer ID being added")
	cmd.Flags().String("out", "", "Volunteer ID being removed")
	cmd.Flags().String("in-custom", "", "Custom value for volunteer being added")
	cmd.Flags().String("out-custom", "", "Custom value for volunteer being removed")
	cmd.Flags().String("swap-date", "", "Date for reverse operation (YYYY-MM-DD)")
	cmd.Flags().String("reason", "", "Reason for the change (required)")

	return cmd
}
