package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// BackfillAvailabilityCmd creates the backfillAvailability command.
//
// This command exists for one run. It copies the availability history out of
// Google Forms and into the stored round, and the very next change deletes the
// Forms client it reads through — so it must be run, against production, from
// this commit and before the contract migration drops the legacy table it walks
// (issue #80). It is safe to run repeatedly until then: nothing is written to
// Google, and a second run recognises everything the first one wrote.
func BackfillAvailabilityCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backfillAvailability",
		Short: "One-off: import availability history from Google Forms into the database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dryRun, _ := cmd.Flags().GetBool("dry-run")

			result, err := services.BackfillAvailabilityFromForms(
				app.Ctx,
				app.Database,
				app.FormsClient,
				app.Logger,
				dryRun,
			)
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Println("\nDry run — nothing was written.")
			}
			fmt.Printf("\nRotas backfilled:      %d\n", result.Rotas)
			fmt.Printf("Forms read:            %d\n", result.FormsRead)
			fmt.Printf("Requests minted:       %d\n", result.RequestsMinted)
			fmt.Printf("Generations written:   %d\n", result.GenerationsWritten)
			fmt.Printf("Generations already in: %d\n", result.GenerationsSkipped)

			if len(result.Warnings) > 0 {
				fmt.Printf("\n%d response(s) could not be imported:\n", len(result.Warnings))
				for _, warning := range result.Warnings {
					fmt.Printf("  - %s\n", warning)
				}
			}

			return nil
		},
	}

	cmd.Flags().Bool("dry-run", false, "Read Forms and report what would be written, without writing anything")

	return cmd
}
