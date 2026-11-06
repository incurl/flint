package wikidata

import "strings"

// QuoteLiteral renders a value as a SPARQL string literal.
//
// A SPARQL literal is delimited by double quotes, and a value that holds a
// double quote would end the literal and change the meaning of the query.
// The backslash is escaped first, so that the escape of the quote is not
// escaped in turn.
//
// The result is safe to concatenate into a query.
func QuoteLiteral(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)

	return `"` + replacer.Replace(value) + `"`
}

// QuoteLanguageLiteral renders a value as a SPARQL string literal with a
// language tag.
//
// The tag is appended to the literal, and it is checked: a tag that is not
// a language tag would end the literal and start a new term.
func QuoteLanguageLiteral(value, language string) string {
	if !isLanguageTag(language) {
		return QuoteLiteral(value)
	}

	return QuoteLiteral(value) + "@" + language
}

// isLanguageTag reports whether a string is a language tag.
//
// The check is the shape of a BCP 47 tag: letters, and the subtags that
// follow a hyphen.
func isLanguageTag(tag string) bool {
	if tag == "" {
		return false
	}

	for _, part := range strings.Split(tag, "-") {
		if part == "" {
			return false
		}

		for _, r := range part {
			if !isTagChar(r) {
				return false
			}
		}
	}

	return true
}

// isTagChar reports whether a rune may appear in a language tag.
func isTagChar(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	default:
		return false
	}
}
