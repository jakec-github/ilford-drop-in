package translatoreval

import "github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"

// price is what a model charges, in US dollars per million tokens.
type price struct {
	input, output, cacheRead, cacheWrite float64
}

// prices is the list price of each model the evals are run against, as listed
// on 2026-11-01. A report prices its runs when they ran, so an old report
// keeps the cost it had. The translators do not cache prompts (ADR 0015), so
// the cache prices are there for when they do; Haiku's are taken as the usual
// tenth and five-quarters of its input price.
var prices = map[string]price{
	"claude-opus-5-5":   {input: 4, output: 20, cacheRead: 0.20, cacheWrite: 5},
	"claude-sonnet-5-5": {input: 2, output: 10, cacheRead: 0.20, cacheWrite: 2.50},
	"claude-haiku-5-5":  {input: 0.10, output: 0.50, cacheRead: 0.01, cacheWrite: 0.125},
}

// cost is what usage costs on model, or nothing for a model with no price.
func cost(model string, u patterntranslator.Usage) float64 {
	p, ok := prices[model]
	if !ok {
		return 0
	}
	return (float64(u.InputTokens)*p.input +
		float64(u.OutputTokens)*p.output +
		float64(u.CacheReadTokens)*p.cacheRead +
		float64(u.CacheWriteTokens)*p.cacheWrite) / 1e6
}
