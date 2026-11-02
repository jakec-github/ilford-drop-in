// Package providers is every llm.Provider the app can ask, by name (ADR 0016).
package providers

import (
	"fmt"
	"strings"

	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
	"github.com/jakechorley/ilford-drop-in/pkg/core/llm/anthropic"
	"github.com/jakechorley/ilford-drop-in/pkg/core/llm/mistral"
)

// All is every provider, in the order they are listed to someone choosing.
var All = []llm.Provider{anthropic.Provider, mistral.Provider}

// Named is the provider called name.
func Named(name string) (llm.Provider, error) {
	for _, p := range All {
		if p.Name == name {
			return p, nil
		}
	}
	return llm.Provider{}, fmt.Errorf("no provider %q: choose %s", name, strings.Join(names(), ", "))
}

// Parse reads a <provider>-<tier>, such as anthropic-medium.
func Parse(ref string) (llm.Provider, llm.Tier, error) {
	name, tier, ok := strings.Cut(ref, "-")
	if ok {
		if p, err := Named(name); err == nil {
			if _, ok := p.Tiers[llm.Tier(tier)]; ok {
				return p, llm.Tier(tier), nil
			}
		}
	}
	return llm.Provider{}, "", fmt.Errorf("no model %q: choose %s", ref, strings.Join(Refs(), ", "))
}

// Refs is every <provider>-<tier>.
func Refs() []string {
	var refs []string
	for _, p := range All {
		for _, t := range llm.Tiers {
			if _, ok := p.Tiers[t]; ok {
				refs = append(refs, p.Name+"-"+string(t))
			}
		}
	}
	return refs
}

func names() []string {
	names := make([]string, len(All))
	for i, p := range All {
		names[i] = p.Name
	}
	return names
}
