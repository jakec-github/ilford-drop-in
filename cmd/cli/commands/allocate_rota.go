package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// AllocateRotaCmd creates the allocateRota command
func AllocateRotaCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allocateRota",
		Short: "Allocate a rota using the CP-SAT solver",
		Long: "Run the Python CP-SAT allocator (pyallocator) to assign volunteers to shifts. " +
			"Hard constraints (availability, Seat capacity per Role, no back-to-back, " +
			"male required, ...) are never violated; soft preferences shape the result to " +
			"fill shifts evenly, spread males and distribute allocations fairly.",
		RunE: func(cmd *cobra.Command, args []string) error {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			forceCommit, _ := cmd.Flags().GetBool("force-commit")
			pythonFlag, _ := cmd.Flags().GetString("python")

			app.Logger.Debug("allocateRota command",
				zap.Bool("dry_run", dryRun),
				zap.Bool("force_commit", forceCommit),
				zap.String("python", pythonFlag))

			result, err := services.AllocateRota(
				app.Ctx,
				app.Database,
				app.SheetsClient,
				app.Cfg,
				app.Logger,
				dryRun,
				forceCommit,
				pythonFlag,
			)
			if err != nil {
				return fmt.Errorf("allocation failed: %w", err)
			}

			// Display header
			fmt.Printf("\n🧮 CP-SAT Rota Allocation Results\n\n")
			fmt.Printf("Rota ID:       %s\n", result.RotaID)
			fmt.Printf("Start Date:    %s\n", result.RotaStart)
			fmt.Printf("Shift Count:   %d\n", result.ShiftCount)
			fmt.Printf("Solver Status: %s\n", result.SolverStatus)
			fmt.Printf("Objective:     %d\n", result.ObjectiveValue)
			fmt.Printf("Solve Time:    %.2fs (%d groups, %d variables)\n",
				result.Diagnostics.SolveTimeSeconds,
				result.Diagnostics.NumGroups,
				result.Diagnostics.NumVariables)
			if dryRun {
				fmt.Printf("Mode:          🧪 DRY RUN (not saved)\n")
			} else if result.Saved {
				fmt.Printf("Status:        ✅ SAVED to database\n")
			} else {
				fmt.Printf("Status:        ❌ NOT SAVED\n")
			}
			fmt.Println()

			if !result.Success {
				fmt.Println("❌ The solver found no rota satisfying every hard constraint (INFEASIBLE).")
				fmt.Println("   CP-SAT never produces a rule-breaking rota, so nothing was written.")
				fmt.Println("   Constraint families to check (usually a preallocation conflicts with one):")
				fmt.Println("   • preallocations vs shift capacity (too many preallocated volunteers for a shift's size)")
				fmt.Println("   • preallocations vs no-back-to-back (same group preallocated to consecutive shifts)")
				fmt.Println("   • preallocations vs max frequency (a group preallocated to more shifts than the cap)")
				fmt.Println("   • preallocations vs a Role's ceiling (more pins for a capped Role than it has Seats)")
				fmt.Println("   • preallocations vs male required (every slot preallocated female, leaving no open slot for a male)")
				return nil
			}

			// Display allocated shifts in the same table style as allocateRota
			fmt.Printf("📅 Allocated Shifts:\n\n")

			const (
				colorReset  = "\033[0m"
				colorGreen  = "\033[32m"
				colorYellow = "\033[33m"
				colorBold   = "\033[1m"
			)

			// Each capped Role gets a column of its own, in priority order;
			// the uncapped Role's holders share the Volunteers column, which
			// is the one the shift's size is counted against.
			roles, err := services.RoleTable(app.Ctx, app.Database)
			if err != nil {
				return err
			}
			cappedRoles := make([]string, 0)
			for _, role := range roles.ByPriority() {
				if role.Capped() {
					cappedRoles = append(cappedRoles, role.Name)
				}
			}

			// Calculate column widths
			cappedColWidths := make(map[string]int, len(cappedRoles))
			for _, name := range cappedRoles {
				cappedColWidths[name] = len(name)
				if cappedColWidths[name] < 15 {
					cappedColWidths[name] = 15
				}
			}
			maxVolunteersLen := 40
			for _, shift := range result.AllocatedShifts {
				capped, ordinary := splitByRole(shift.Assignments, cappedRoles)
				for _, name := range cappedRoles {
					if width := len(strings.Join(capped[name], ", ")); width > cappedColWidths[name] {
						cappedColWidths[name] = width
					}
				}

				totalLen := 0
				for _, assignment := range ordinary {
					totalLen += len(assignmentName(assignment)) + 2
				}
				if totalLen > maxVolunteersLen {
					maxVolunteersLen = totalLen
				}
			}

			dateColWidth := 12
			for _, name := range cappedRoles {
				cappedColWidths[name] += 2
			}
			volunteersColWidth := maxVolunteersLen + 2

			fmt.Printf("%s%-*s", colorBold, dateColWidth, "Date")
			for _, name := range cappedRoles {
				fmt.Printf("  %-*s", cappedColWidths[name], name)
			}
			fmt.Printf("  %-*s  %s%s\n", volunteersColWidth, "Volunteers", "Size", colorReset)

			fmt.Print(strings.Repeat("-", dateColWidth))
			for _, name := range cappedRoles {
				fmt.Print("  ")
				fmt.Print(strings.Repeat("-", cappedColWidths[name]))
			}
			fmt.Print("  ")
			fmt.Print(strings.Repeat("-", volunteersColWidth))
			fmt.Print("  ")
			fmt.Println("----")

			for _, shift := range result.AllocatedShifts {
				fmt.Printf("%-*s", dateColWidth, shift.Date)

				capped, ordinary := splitByRole(shift.Assignments, cappedRoles)
				for _, name := range cappedRoles {
					cell := "—"
					if len(capped[name]) > 0 {
						cell = strings.Join(capped[name], ", ")
					}
					// The colour codes are invisible but counted, so the
					// padding is written out rather than left to %-*s.
					padding := strings.Repeat(" ", cappedColWidths[name]-len(cell))
					if len(capped[name]) > 0 {
						cell = fmt.Sprintf("%s%s%s", colorGreen, cell, colorReset)
					}
					fmt.Printf("  %s%s", cell, padding)
				}

				volunteers := make([]string, 0, len(ordinary))
				for _, assignment := range ordinary {
					name := assignmentName(assignment)
					if assignment.Custom != "" {
						name = fmt.Sprintf("%s[%s]%s", colorYellow, name, colorReset)
					}
					volunteers = append(volunteers, name)
				}

				volunteersStr := "—"
				if shift.Closed {
					volunteersStr = "(closed)"
				} else if len(volunteers) > 0 {
					volunteersStr = strings.Join(volunteers, ", ")
				}
				fmt.Printf("  %-*s  ", volunteersColWidth, volunteersStr)

				sizeStr := fmt.Sprintf("%d/%d", len(ordinary), shift.Size)
				if len(ordinary) == shift.Size {
					sizeStr = fmt.Sprintf("%s%s%s", colorGreen, sizeStr, colorReset)
				}
				fmt.Printf("%s\n", sizeStr)
			}
			fmt.Println()

			if dryRun {
				fmt.Println("💡 This was a dry run. Use without --dry-run to save allocations.")
			} else if result.Saved {
				fmt.Println("✅ Allocations have been saved to the database.")
			}

			return nil
		},
	}

	cmd.Flags().Bool("dry-run", false, "Run without saving to database")
	cmd.Flags().Bool("force-commit", false, "Save allocations even if the solver found no feasible rota")
	cmd.Flags().String("python", "", "Python interpreter to run pyallocator with (default: $ILFORD_CPSAT_PYTHON, then pyallocator/.venv/bin/python, then python3)")

	return cmd
}

// splitByRole separates a solved shift's filled Seats into the ones in a capped
// Role, which the table gives their own columns, and everything else in
// assignment order.
func splitByRole(assignments []allocator.Assignment, cappedRoles []string) (map[string][]string, []allocator.Assignment) {
	isCapped := make(map[string]bool, len(cappedRoles))
	for _, name := range cappedRoles {
		isCapped[name] = true
	}

	capped := make(map[string][]string, len(cappedRoles))
	ordinary := make([]allocator.Assignment, 0, len(assignments))
	for _, assignment := range assignments {
		if isCapped[assignment.Role] {
			capped[assignment.Role] = append(capped[assignment.Role], assignmentName(assignment))
			continue
		}
		ordinary = append(ordinary, assignment)
	}
	return capped, ordinary
}

// assignmentName is what to print for a filled Seat: the volunteer in it,
// or the free text a custom entry holds.
func assignmentName(assignment allocator.Assignment) string {
	if assignment.Volunteer != nil {
		return assignment.Volunteer.DisplayName
	}
	return assignment.Custom
}
