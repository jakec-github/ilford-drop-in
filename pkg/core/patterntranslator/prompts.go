package patterntranslator

import (
	_ "embed"

	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
)

// The playground version of the pattern prompt in docs/design/llm/ is for
// trying it by hand — these are the ones that run.
var (
	//go:embed pattern.md
	patternSystem string
	//go:embed pattern.schema.json
	patternSchema []byte
	//go:embed rule.md
	ruleSystem string
	//go:embed rule.schema.json
	ruleSchema []byte
)

var (
	// patternPrompt writes a Shift pattern's VEVENT entries.
	patternPrompt = llm.NewPrompt("pattern", patternSystem, patternSchema)
	// rulePrompt writes what an Override matches: a DTSTART date and an RRULE.
	rulePrompt = llm.NewPrompt("rule", ruleSystem, ruleSchema)
)

// Prompts is every prompt a translator asks under, for whatever measures them:
// an evaluation names the versions it ran against.
func Prompts() []*llm.Prompt {
	return []*llm.Prompt{patternPrompt, rulePrompt}
}
