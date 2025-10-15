package rotageneration

// ValidateRotaState validates the final rota state against all provided criteria.
// Returns a slice of validation errors for any constraint violations.
// An empty slice indicates the rota is valid.
func ValidateRotaState(state *RotaState, criteria []Criterion) []ShiftValidationError {
	var errors []ShiftValidationError

	// Run validation for each criterion
	for _, criterion := range criteria {
		criterionErrors := criterion.ValidateRotaState(state)
		errors = append(errors, criterionErrors...)
	}

	return errors
}
