package providers

import (
	"slices"
	"strings"
	"testing"

	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
)

func TestParse_ReadsAProviderAndTier(t *testing.T) {
	for ref, want := range map[string]string{"anthropic-medium": "claude-sonnet-5-5", "mistral-small": "mistral-small-2603"} {
		p, tier, err := Parse(ref)
		if err != nil {
			t.Fatalf("Parse(%s): %v", ref, err)
		}
		if p.Name+"-"+string(tier) != ref || p.Tiers[tier].ID != want {
			t.Errorf("Parse(%s) = %s %s %s", ref, p.Name, tier, p.Tiers[tier].ID)
		}
	}
}

func TestParse_RefusesWhatItDoesNotKnowAndListsWhatItDoes(t *testing.T) {
	for _, ref := range []string{"claude-sonnet-5-5", "anthropic-huge", "anthropic", "nobody-small", ""} {
		_, _, err := Parse(ref)
		if err == nil {
			t.Errorf("Parse(%q) = no error", ref)
			continue
		}
		if !strings.Contains(err.Error(), "anthropic-small, anthropic-medium, anthropic-large") {
			t.Errorf("Parse(%q): %v, want the choices listed", ref, err)
		}
	}
}

func TestNamed_RefusesAnUnknownProviderAndListsTheKnown(t *testing.T) {
	for _, name := range []string{"anthropic", "mistral"} {
		if _, err := Named(name); err != nil {
			t.Errorf("Named(%s): %v", name, err)
		}
	}
	_, err := Named("openai")
	if err == nil || !strings.Contains(err.Error(), "anthropic, mistral") {
		t.Errorf("Named(openai): %v, want the known ones listed", err)
	}
}

// Every provider answers every tier, so a tier named in code is never missing
// on the provider a deployment chose.
func TestAll_EveryProviderAnswersEveryTierAtAPrice(t *testing.T) {
	for _, p := range All {
		for _, tier := range llm.Tiers {
			m, ok := p.Tiers[tier]
			if !ok || m.ID == "" || m.Price.Input == 0 || m.Price.Output == 0 {
				t.Errorf("%s-%s: %+v", p.Name, tier, m)
			}
		}
		if p.KeyEnv == "" || p.New == nil {
			t.Errorf("%s: no key variable or constructor", p.Name)
		}
	}
	if !slices.Equal(Refs(), []string{
		"anthropic-small", "anthropic-medium", "anthropic-large",
		"mistral-small", "mistral-medium", "mistral-large",
	}) {
		t.Errorf("Refs() = %v", Refs())
	}
}
