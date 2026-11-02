package llm

import "testing"

func TestPrice_CostsPerMillionTokensAndNothingWithoutAPrice(t *testing.T) {
	p := &Price{Input: 2, Output: 10, CacheRead: 0.2, CacheWrite: 2.5}
	got := p.Cost(Usage{InputTokens: 1_000_000, OutputTokens: 100_000, CacheReadTokens: 1_000_000, CacheWriteTokens: 0})
	if got != 3.2 {
		t.Errorf("Cost = %v, want 3.2", got)
	}
	var none *Price
	if none.Cost(Usage{InputTokens: 1000}) != 0 {
		t.Error("a nil Price costs something")
	}
}
