package allocator

// Built-in ranking weights for volunteer group prioritization
const (
	// WeightCurrentRotaUrgency is the weight applied based on how much of the current rota's
	// allocation budget the group needs to use up. Higher values prioritize groups that need
	// to be allocated frequently in this rota to stay on track.
	WeightCurrentRotaUrgency = 1

	// WeightOverallFrequencyFairness is the weight applied based on how many allocations
	// the group needs to reach their target frequency over time (historical + current).
	// Higher values prioritize fairness across all rotas.
	WeightOverallFrequencyFairness = 1

	// WeightPromoteGroup is the weight applied to groups over individuals.
	// Higher values prioritise groups more strongly. Group size does not affect score
	WeightPromoteGroup = 1
)
