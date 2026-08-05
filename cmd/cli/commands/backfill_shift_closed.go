package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// BackfillShiftClosedCmd is the one-off that moves closure from the config's
// rrules onto the Shift rows (issue #132).
//
// This commit exists to be checked out and run from, once, against each
// environment, after #132 has merged. The next commit deletes both this and the
// config key it reads, so the schema here matches main's — the migration set is
// identical, and running the CLI from here applies nothing the deploy has not
// already applied.
//
// It is built to be re-run rather than to be got right first time: it only ever
// closes, so a second run reports what it found and changes nothing.
func BackfillShiftClosedCmd(app *AppContext) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "backfillShiftClosed",
		Short: "One-off: stamp the config's closed rrules onto existing shifts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := services.BackfillShiftClosed(app.Ctx, app.Database, app.Cfg, dryRun, app.Logger)
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Printf("\nDry run — nothing was written.\n\n")
			} else {
				fmt.Printf("\n✓ Backfill complete.\n\n")
			}
			fmt.Printf("Shifts scanned:      %d\n", result.Scanned)
			fmt.Printf("Closed by this run:  %d\n", len(result.Closed))
			fmt.Printf("Already closed:      %d\n\n", result.AlreadyClosed)

			if len(result.Closed) > 0 {
				fmt.Printf("Dates closed:\n")
				for _, date := range result.Closed {
					fmt.Printf("  %s\n", date)
				}
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be closed without writing anything")

	return cmd
}
