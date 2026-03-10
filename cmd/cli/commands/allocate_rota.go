package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	allocator "github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// AllocateRotaCmd creates the allocateRota command
func AllocateRotaCmd(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "allocateRota",
		Short: "Allocate a rota from availability responses",
		Long:  "Run the allocation algorithm to assign volunteers to shifts based on availability responses",
		RunE: func(cmd *cobra.Command, args []string) error {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			forceCommit, _ := cmd.Flags().GetBool("force-commit")

			app.Logger.Debug("allocateRota command",
				zap.Bool("dry_run", dryRun),
				zap.Bool("force_commit", forceCommit))

			// Call the service
			result, err := services.AllocateRota(
				app.Ctx,
				app.Database,
				app.SheetsClient,
				app.FormsClient,
				app.Cfg,
				app.Logger,
				dryRun,
				forceCommit,
			)
			if err != nil {
				return fmt.Errorf("allocation failed: %w", err)
			}

			// Display header
			fmt.Printf("\n🎯 Rota Allocation Results\n\n")
			fmt.Printf("Rota ID:     %s\n", result.RotaID)
			fmt.Printf("Start Date:  %s\n", result.RotaStart)
			fmt.Printf("Shift Count: %d\n", result.ShiftCount)
			if dryRun {
				fmt.Printf("Mode:        🧪 DRY RUN (not saved)\n")
			} else if result.Success {
				fmt.Printf("Status:      ✅ SUCCESS (saved to database)\n")
			} else if forceCommit {
				fmt.Printf("Status:      ⚠️  FORCED (saved despite validation errors)\n")
			} else if result.Status == allocator.RotaStatusIncomplete {
				fmt.Printf("Status:      ⚠️  INCOMPLETE (not saved — add volunteers to fix)\n")
			} else {
				fmt.Printf("Status:      ❌ INVALID (not saved — volunteers must be removed)\n")
			}
			fmt.Println()

			// Display validation errors if any
			if len(result.ValidationErrors) > 0 {
				fmt.Printf("⚠️  Validation Errors (%d):\n", len(result.ValidationErrors))
				for _, verr := range result.ValidationErrors {
					label := "INCOMPLETE"
					if verr.Type == allocator.ValidationErrorTypeInvalid {
						label = "INVALID"
					}
					fmt.Printf("  • [%s] Shift %d (%s) - %s: %s\n",
						label,
						verr.ShiftIndex+1,
						verr.ShiftDate,
						verr.CriterionName,
						verr.Description)
				}
				fmt.Println()
			}

			// Display allocated shifts in a table
			fmt.Printf("📅 Allocated Shifts:\n\n")

			// ANSI color codes
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

				// Calculate total volunteer names length
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

			// Print header
			fmt.Printf("%s%-*s  %-*s  %-*s  %s%s\n",
				colorBold,
				dateColWidth, "Date",
				teamLeadColWidth, "Team Lead",
				volunteersColWidth, "Volunteers",
				"Size",
				colorReset)

			// Print separator
			fmt.Print(strings.Repeat("-", dateColWidth))
			fmt.Print("  ")
			fmt.Print(strings.Repeat("-", teamLeadColWidth))
			fmt.Print("  ")
			fmt.Print(strings.Repeat("-", volunteersColWidth))
			fmt.Print("  ")
			fmt.Println("----")

			// Print each shift
			for _, shift := range result.AllocatedShifts {
				// Format date
				fmt.Printf("%-*s  ", dateColWidth, shift.Date)

				// Format team lead
				teamLeadStr := "—"
				if shift.TeamLead != nil {
					teamLeadStr = fmt.Sprintf("%s%s%s",
						colorGreen,
						shift.TeamLead.DisplayName,
						colorReset)
				}
				// Calculate display width without color codes
				teamLeadDisplayWidth := 0
				if shift.TeamLead != nil {
					teamLeadDisplayWidth = len(shift.TeamLead.DisplayName)
				} else {
					teamLeadDisplayWidth = 1
				}
				fmt.Printf("%s%s  ", teamLeadStr, strings.Repeat(" ", teamLeadColWidth-teamLeadDisplayWidth))

				// Format volunteers (excluding team lead)
				volunteers := []string{}
				for _, group := range shift.AllocatedGroups {
					for _, member := range group.Members {
						// Skip if this member is the team lead
						if shift.TeamLead != nil && member.ID == shift.TeamLead.ID {
							continue
						}
						volunteers = append(volunteers, member.DisplayName)
					}
				}

				// Add pre-allocated volunteers
				for _, preAlloc := range shift.CustomPreallocations {
					volunteers = append(volunteers, fmt.Sprintf("%s[%s]%s", colorYellow, preAlloc, colorReset))
				}

				volunteersStr := "—"
				if len(volunteers) > 0 {
					volunteersStr = strings.Join(volunteers, ", ")
				}
				fmt.Printf("%-*s  ", volunteersColWidth, volunteersStr)

				// Format size
				sizeStr := fmt.Sprintf("%d/%d", shift.CurrentSize(), shift.Size)
				if shift.CurrentSize() == shift.Size {
					sizeStr = fmt.Sprintf("%s%s%s", colorGreen, sizeStr, colorReset)
				}
				fmt.Printf("%s\n", sizeStr)
			}
			fmt.Println()

			// Display underutilized groups if any
			if len(result.UnderutilizedGroups) > 0 {
				fmt.Printf("ℹ️  Underutilized Groups (%d):\n", len(result.UnderutilizedGroups))
				fmt.Println("  (Groups with remaining availability that weren't fully allocated)")
				for _, group := range result.UnderutilizedGroups {
					allocated := len(group.AllocatedShiftIndices)
					available := min(maxAllocationCount(result.ShiftCount, app.Cfg.MaxAllocationFrequency), len(group.AvailableShiftIndices))
					// Use DisplayName for individual volunteers, GroupKey for actual groups
					displayName := group.GroupKey
					if len(group.Members) == 1 {
						displayName = group.Members[0].DisplayName
					}
					fmt.Printf("  • %s: allocated %d/%d shifts\n", displayName, allocated, available)
				}
				fmt.Println()
			}

			// Summary message
			if dryRun {
				fmt.Println("💡 This was a dry run. Use without --dry-run to save allocations.")
			} else if result.Success {
				fmt.Println("✅ Allocations have been saved to the database.")
			} else if forceCommit {
				fmt.Println("⚠️  Allocations were saved despite validation errors (--force-commit).")
			} else {
				fmt.Println("❌ Allocations were not saved due to validation errors.")
				fmt.Println("💡 Use --force-commit to save anyway, or fix the issues and try again.")
			}

			return nil
		},
	}

	cmd.Flags().Bool("dry-run", false, "Run without saving to database")
	cmd.Flags().Bool("force-commit", false, "Save allocations even if validation fails")

	return cmd
}

func maxAllocationCount(shiftCount int, maxAllocationFrequency float64) int {
	return int(float64(shiftCount) * maxAllocationFrequency)
}
