package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// AllocateRotaCmd creates the allocateRota command
func AllocateRotaCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allocateRota",
		Short: "Allocate a rota using the CP-SAT solver",
		Long: "Run the Python CP-SAT allocator (pyallocator) to assign volunteers to shifts. " +
			"Hard constraints (availability, capacity, no back-to-back, max one team lead, " +
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
				app.FormsClient,
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
				fmt.Println("   • preallocations vs team leads (two team leads preallocated onto one shift)")
				fmt.Println("   • preallocations vs male required (every slot preallocated female, leaving no open slot for a male)")
				fmt.Println("   • closed shifts (an override closing a shift that another override populates)")
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

			// Calculate column widths
			maxTeamLeadLen := 15
			maxVolunteersLen := 40
			for _, shift := range result.AllocatedShifts {
				if shift.TeamLead != nil {
					nameLen := len(shift.TeamLead.DisplayName)
					if nameLen > maxTeamLeadLen {
						maxTeamLeadLen = nameLen
					}
				}

				totalLen := 0
				for _, group := range shift.AllocatedGroups {
					for _, member := range group.Members {
						if shift.TeamLead == nil || member.ID != shift.TeamLead.ID {
							totalLen += len(member.DisplayName) + 2
						}
					}
				}
				if totalLen > maxVolunteersLen {
					maxVolunteersLen = totalLen
				}
			}

			dateColWidth := 12
			teamLeadColWidth := maxTeamLeadLen + 2
			volunteersColWidth := maxVolunteersLen + 2

			fmt.Printf("%s%-*s  %-*s  %-*s  %s%s\n",
				colorBold,
				dateColWidth, "Date",
				teamLeadColWidth, "Team Lead",
				volunteersColWidth, "Volunteers",
				"Size",
				colorReset)

			fmt.Print(strings.Repeat("-", dateColWidth))
			fmt.Print("  ")
			fmt.Print(strings.Repeat("-", teamLeadColWidth))
			fmt.Print("  ")
			fmt.Print(strings.Repeat("-", volunteersColWidth))
			fmt.Print("  ")
			fmt.Println("----")

			for _, shift := range result.AllocatedShifts {
				fmt.Printf("%-*s  ", dateColWidth, shift.Date)

				teamLeadStr := "—"
				teamLeadDisplayWidth := 1
				if shift.TeamLead != nil {
					teamLeadStr = fmt.Sprintf("%s%s%s", colorGreen, shift.TeamLead.DisplayName, colorReset)
					teamLeadDisplayWidth = len(shift.TeamLead.DisplayName)
				}
				fmt.Printf("%s%s  ", teamLeadStr, strings.Repeat(" ", teamLeadColWidth-teamLeadDisplayWidth))

				volunteers := []string{}
				for _, group := range shift.AllocatedGroups {
					for _, member := range group.Members {
						if shift.TeamLead != nil && member.ID == shift.TeamLead.ID {
							continue
						}
						volunteers = append(volunteers, member.DisplayName)
					}
				}
				for _, preAlloc := range shift.CustomPreallocations {
					volunteers = append(volunteers, fmt.Sprintf("%s[%s]%s", colorYellow, preAlloc, colorReset))
				}

				volunteersStr := "—"
				if shift.Closed {
					volunteersStr = "(closed)"
				} else if len(volunteers) > 0 {
					volunteersStr = strings.Join(volunteers, ", ")
				}
				fmt.Printf("%-*s  ", volunteersColWidth, volunteersStr)

				sizeStr := fmt.Sprintf("%d/%d", shift.CurrentSize(), shift.Size)
				if shift.CurrentSize() == shift.Size {
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
