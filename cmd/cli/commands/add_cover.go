package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// AddCoverCmd creates the addCover command
func AddCoverCmd(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "addCover <shift_date> <covered_volunteer_id> <covering_volunteer_id> [rota_id]",
		Short: "Add a cover/swap for a shift",
		Args:  cobra.RangeArgs(3, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			shiftDate := args[0]
			// coveredVolunteerID := args[1]
			// coveringVolunteerID := args[2]
			// var rotaID string
			// if len(args) > 3 {
			// 	rotaID = args[3]
			// }

			fmt.Printf("TODO: Implement addCover for shift %s\n", shiftDate)
			// Service call will go here: services.AddCover(app.Ctx, app.Cfg, app.SheetsClient, app.Database, shiftDate, coveredVolunteerID, coveringVolunteerID, rotaID)
			return nil
		},
	}
}
