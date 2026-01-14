package e2e

import (
	allocator "github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator/criteria"
)

// Type aliases to avoid prefixing everything with allocator.
type (
	Volunteer             = allocator.Volunteer
	VolunteerAvailability = allocator.VolunteerAvailability
	VolunteerGroup        = allocator.VolunteerGroup
	VolunteerState        = allocator.VolunteerState
	Shift                 = allocator.Shift
	RotaState             = allocator.RotaState
	Criterion             = allocator.Criterion
	AllocationConfig      = allocator.AllocationConfig
	ShiftOverride         = allocator.ShiftOverride
)

// Function aliases
var (
	Allocate                   = allocator.Allocate
	ValidateRotaState          = allocator.ValidateRotaState
	NewShiftSizeCriterion      = criteria.NewShiftSizeCriterion
	NewTeamLeadCriterion       = criteria.NewTeamLeadCriterion
	NewMaleBalanceCriterion    = criteria.NewMaleBalanceCriterion
	NewNoDoubleShiftsCriterion = criteria.NewNoDoubleShiftsCriterion
	NewShiftSpreadCriterion    = criteria.NewShiftSpreadCriterion
)
