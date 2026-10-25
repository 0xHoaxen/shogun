package resume

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	spaceRun = regexp.MustCompile(`[ \t\x{00A0}]{2,}`)
	blankRun = regexp.MustCompile(`\n{3,}`)
	// trailing space on a line.
	lineEnd = regexp.MustCompile(` +\n`)
)

// clean makes extracted text uniform: one kind of line end, no control
// characters, no runs of spaces, no more than one blank line in a row, no space
// around the whole. It returns the text cut to MaxChars, and whether it was cut.
func clean(text string) (string, bool) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == 0x2028 || r == 0x2029: // line and paragraph separators
			return '\n'
		case r == unicode.ReplacementChar || unicode.IsControl(r) || r == 0xFEFF || r == 0x200B:
			return -1
		}
		return r
	}, text)
	text = spaceRun.ReplaceAllString(text, " ")
	text = lineEnd.ReplaceAllString(text, "\n")
	text = blankRun.ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)

	runes := []rune(text)
	if len(runes) > MaxChars {
		return strings.TrimSpace(string(runes[:MaxChars])), true
	}
	return text, false
}
