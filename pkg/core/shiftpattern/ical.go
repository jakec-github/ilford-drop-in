package shiftpattern

import (
	"strings"
)

// A pattern and an Override's rule are both written as iCalendar content lines
// (RFC 5545 §3.1): NAME;PARAM=VALUE:VALUE, one to a line. This is the little of
// that grammar both need. It is read leniently, as pasted text should be, and
// the readers above it decide what each line may say.

// contentLine is one logical line, unfolded.
type contentLine struct {
	// Number is the line it starts on in the text as typed, counting from one.
	Number int
	// Raw is the line as written, unfolded and trimmed.
	Raw string
	// Name is the property name in upper case: DTSTART, RRULE, BEGIN.
	Name string
	// Params are the parameters, names in upper case: TZID, VALUE.
	Params map[string]string
	// Value is everything after the first colon. For a line with no colon it
	// is the whole line, so a bare FREQ=WEEKLY still reads as an RRULE's value.
	Value    string
	HasColon bool
}

// contentLines unfolds text and splits it into its non-blank logical lines.
//
// A line starting with a space or a tab carries on the one before it, which is
// how a calendar wraps a long line (RFC 5545 §3.1). The fold is undone here
// and never written: what is stored is the text as the Organiser gave it.
func contentLines(text string) []contentLine {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var lines []contentLine
	for i, raw := range strings.Split(text, "\n") {
		if (strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t")) && len(lines) > 0 && strings.TrimSpace(raw) != "" {
			last := &lines[len(lines)-1]
			last.Raw += strings.TrimRight(raw[1:], " \t")
			continue
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		lines = append(lines, contentLine{Number: i + 1, Raw: trimmed})
	}
	for i := range lines {
		lines[i].split()
	}
	return lines
}

// split reads a line's name, parameters and value out of its raw text.
func (l *contentLine) split() {
	head, value, found := cutOutsideQuotes(l.Raw, ':')
	l.HasColon = found
	if !found {
		head, value = "", l.Raw
	}
	l.Value = strings.TrimSpace(value)
	l.Params = map[string]string{}

	parts := strings.Split(head, ";")
	l.Name = strings.ToUpper(strings.TrimSpace(parts[0]))
	for _, p := range parts[1:] {
		key, val, _ := strings.Cut(p, "=")
		l.Params[strings.ToUpper(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(val), `"`)
	}
}

// cutOutsideQuotes is strings.Cut at the first sep not inside double quotes,
// since a parameter value may quote a colon (TZID="a:b").
func cutOutsideQuotes(s string, sep byte) (before, after string, found bool) {
	quoted := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			quoted = !quoted
		case sep:
			if !quoted {
				return s[:i], s[i+1:], true
			}
		}
	}
	return s, "", false
}

// isRuleLine is whether a line is an RRULE: one named so, or a bare rule
// written without its name, as an Override's rule always could be.
func (l contentLine) isRuleLine() bool {
	if l.Name == "RRULE" {
		return true
	}
	return !l.HasColon && strings.HasPrefix(strings.ToUpper(l.Value), "FREQ=")
}

// refusedProperty is why a property no entry or rule may hold is refused, or
// "" for one that is not refused outright.
func refusedProperty(name string) string {
	switch name {
	case "RDATE":
		return "RDATE adds a one-off shift, and every shift comes from the pattern"
	case "EXDATE":
		return "EXDATE is not supported - close a shift by hand instead"
	case "EXRULE":
		return "EXRULE is not supported - close a shift by hand instead"
	}
	return ""
}
