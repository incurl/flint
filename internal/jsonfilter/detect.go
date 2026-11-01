package jsonfilter

import "strings"

// NeedsLocal reports whether the expression uses a JSON construct that
// the native PocketBase filter grammar cannot evaluate.
//
// The native grammar reaches into nested objects of a JSON field, so
// `meta.lang = "go"` is evaluated by the database. It cannot reach into
// an array: the expression parser rejects the `[` character, and an
// array field compared with `=` matches nothing.
//
// The detection is syntactic on purpose. The native parser reports a
// malformed expression with a bare error that is not wrapped in
// [pbstore.ErrInvalidFilter], so it cannot be recognised reliably from
// the error alone. Deciding up front keeps the behaviour identical on
// both backends and avoids a failed round trip to the server.
func NeedsLocal(expr string) bool {
	for _, group := range splitClauses(expr, "||") {
		for _, part := range splitClauses(group, "&&") {
			if clauseNeedsLocal(part) {
				return true
			}
		}
	}

	return false
}

// clauseNeedsLocal reports whether a single comparison uses a construct
// that the native grammar cannot evaluate.
//
// Only the left operand is inspected: a bracket inside a quoted value is
// a literal, not an array index.
func clauseNeedsLocal(expr string) bool {
	expr = strings.TrimSpace(expr)

	// A negation is not a native operator, so the whole clause is
	// evaluated locally.
	if strings.HasPrefix(expr, "not ") {
		return true
	}

	path := leftOperand(expr)
	if path == "" {
		// A pure selector without an operator: "tags.#(...)".
		return strings.Contains(expr, "#(")
	}

	// A gjson selector: "tags.#(...)".
	if strings.Contains(path, "#(") {
		return true
	}

	// An array index: "tags[0]".
	if strings.Contains(path, "[") || strings.Contains(path, "]") {
		return true
	}

	// A path segment that walks into an array without the bracket
	// notation: "items.0.name".
	return hasArraySegment(path)
}

// hasArraySegment reports whether the path contains a segment that
// addresses an array element.
//
// The left operand is the text before the comparison operator.
func hasArraySegment(path string) bool {
	if path == "" {
		return false
	}

	for _, segment := range strings.Split(path, ".") {
		if segment == "" {
			continue
		}

		// A numeric segment addresses an array element: "items.0.name".
		if isNumeric(segment) {
			return true
		}

		// The "#" wildcard addresses every element of an array:
		// "items.#.qty".
		if segment == "#" {
			return true
		}
	}

	return false
}

// leftOperand returns the path of the left operand of a comparison.
//
// The operator is looked up outside the quoted values, so that a value
// holding an operator does not cut the path short.
func leftOperand(expr string) string {
	inQuote := false

	for i := 0; i < len(expr); i++ {
		c := expr[i]

		if c == '"' {
			inQuote = !inQuote

			continue
		}

		if inQuote {
			continue
		}

		for _, op := range operators {
			if !strings.HasPrefix(expr[i:], op) {
				continue
			}

			if i > 0 && expr[i-1] == ' ' &&
				i+len(op) < len(expr) && expr[i+len(op)] == ' ' {
				return strings.TrimSpace(expr[:i])
			}
		}
	}

	return ""
}

// isNumeric reports whether the segment is a decimal number.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}

	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}

	return true
}

// splitClauses splits an expression on a separator, ignoring the
// separators inside a quoted value or inside a "#(...)" selector.
func splitClauses(expr, separator string) []string {
	var (
		parts   []string
		current strings.Builder
		inQuote bool
		inQuery bool
	)

	for i := 0; i < len(expr); i++ {
		c := expr[i]

		switch {
		case (c == '"' || c == '\'') && !inQuery:
			inQuote = !inQuote
		case c == '(' && !inQuote:
			inQuery = true
		case c == ')' && !inQuote:
			inQuery = false
		}

		if !inQuote && !inQuery && strings.HasPrefix(expr[i:], separator) {
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()

			i += len(separator) - 1

			continue
		}

		current.WriteByte(c)
	}

	if strings.TrimSpace(current.String()) != "" {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	return parts
}
