package allocator

// ShiftValidationError represents a validation error for a specific shift
type ShiftValidationError struct {
	ShiftIndex    int
	ShiftDate     string
	CriterionName string
	Description   string
}

// Criterion defines the interface for custom allocation criteria
// Criteria influence both which volunteer groups are prioritized and which shifts they're assigned to
type Criterion interface {
	// Name returns a human-readable identifier for this criterion
	Name() string

	// PromoteVolunteerGroup adjusts the priority ranking of a volunteer group
	// Returns a score between -1.0 and 1.0 that will be multiplied by the criterion's group weight
	// Positive values increase priority (allocate earlier), negative values decrease it
	// Return 0 if this criterion doesn't affect group ranking
	PromoteVolunteerGroup(state *RotaState, group *VolunteerGroup) float64

	// IsShiftValid determines if a shift is valid for allocation to a volunteer group
	// Returns false if allocating the group to this shift would violate a hard constraint
	// This acts as a veto - if ANY criterion returns false, the shift cannot be allocated
	IsShiftValid(state *RotaState, group *VolunteerGroup, shift *Shift) bool

	// CalculateShiftAffinity calculates how well a shift matches a volunteer group
	// Returns a score between 0.0 and 1.0 that will be multiplied by the criterion's affinity weight
	// Higher scores indicate better matches and will be preferred during allocation
	// Return 0 if this criterion doesn't affect shift selection
	CalculateShiftAffinity(state *RotaState, group *VolunteerGroup, shift *Shift) float64

	// ValidateRotaState checks if the final rota state meets this criterion's requirements
	// Returns a slice of validation errors (empty if all valid)
	// This is called after allocation completes to verify the rota satisfies all constraints
	ValidateRotaState(state *RotaState) []ShiftValidationError

	// GroupWeight returns the weight for group promotion (typical range: 0.0 - 10.0)
	// Higher weights make this criterion's group promotion more influential
	GroupWeight() float64

	// AffinityWeight returns the weight for shift affinity (typical range: 0.0 - 10.0)
	// Higher weights make this criterion's affinity calculation more influential
	AffinityWeight() float64
}
