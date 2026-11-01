package patterntranslator

import (
	_ "embed"
	"encoding/json"
)

// Prompt is what a Model is asked under: a system prompt, the same on every
// call, and the reply shapes the API's structured outputs enforce, so a reply
// always parses and cannot be both ok and issues. Everything that varies goes
// in the user turn.
type Prompt struct {
	Name   string
	System string
	Schema map[string]any
}

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
	patternPrompt = newPrompt("pattern", patternSystem, patternSchema)
	// rulePrompt writes what an Override matches: a DTSTART date and an RRULE.
	rulePrompt = newPrompt("rule", ruleSystem, ruleSchema)
)

func newPrompt(name, system string, schemaJSON []byte) *Prompt {
	var schema map[string]any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		panic("patterntranslator: " + name + ".schema.json: " + err.Error())
	}
	return &Prompt{Name: name, System: system, Schema: schema}
}
