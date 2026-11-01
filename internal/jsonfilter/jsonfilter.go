// Package jsonfilter evaluates filter expressions against JSON documents.
//
// It exists to cover the gap between what the native PocketBase filter
// grammar can express and what a JSON document can actually contain.
//
// The native grammar reaches into nested objects of a JSON field, so
// `meta.lang = "go"` and `meta.level > 2` are evaluated by the database
// itself. It cannot, however, reach into an array: the expression parser
// rejects the `[` character outright, and an array field compared with
// `=` matches nothing.
//
// This package evaluates the expressions the native grammar cannot:
// `meta.tags.#(=="cli")` (an array contains a value), `meta.items.0.name`
// (an array index) and `meta.items.#.qty > 5` (a numeric comparison
// across the elements of an array).
//
// The supported syntax deliberately mirrors the PocketBase grammar so
// that a single expression can be handed to whichever layer can handle
// it:
//
//	meta.lang = "go"          equality
//	meta.lang != "go"         inequality
//	meta.level > 2           numeric comparison
//	meta.level >= 2          numeric comparison
//	meta.level < 2           numeric comparison
//	meta.level <= 2          numeric comparison
//	meta.lang ~ "g"          contains
//	meta.lang = "go" && meta.level > 2   conjunction
//	meta.lang = "go" || meta.level > 2   disjunction
//
// The array extensions use the gjson path syntax:
//
//	meta.tags.#(=="cli")      any element equals "cli"
//	meta.items.0.name         the "name" of the first element
//	meta.items.#.qty          the "qty" of every element
package jsonfilter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// Filter is a compiled filter expression.
//
// A Filter is immutable and safe for concurrent use.
type Filter struct {
	// expr is the original expression.
	expr string
	// clauses holds the top-level disjunction groups.
	clauses [][]clause
}

// clause is a single comparison, optionally negated.
type clause struct {
	// path is the JSON path of the left operand.
	path string
	// op is the comparison operator.
	op string
	// value is the right operand, as written in the expression.
	value string
	// negated reports whether the comparison is wrapped in "not".
	negated bool
	// query is the gjson query expression of the left operand, when the
	// path carries a "#(...)" selector.
	query string
}

// Parse compiles a filter expression.
//
// It returns an error when the expression is malformed.
func Parse(expr string) (*Filter, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return nil, fmt.Errorf("jsonfilter: the expression is empty")
	}

	groups, err := splitTop(trimmed, "||")
	if err != nil {
		return nil, err
	}

	filter := &Filter{expr: trimmed}

	for _, group := range groups {
		parts, err := splitTop(group, "&&")
		if err != nil {
			return nil, err
		}

		parsed := make([]clause, 0, len(parts))

		for _, part := range parts {
			c, err := parseClause(strings.TrimSpace(part))
			if err != nil {
				return nil, err
			}

			parsed = append(parsed, c)
		}

		filter.clauses = append(filter.clauses, parsed)
	}

	return filter, nil
}

// Expr returns the original expression.
func (f *Filter) Expr() string {
	if f == nil {
		return ""
	}

	return f.expr
}

// Match reports whether the JSON document satisfies the filter.
//
// A document that is not valid JSON never matches.
func (f *Filter) Match(document string) bool {
	if f == nil {
		return true
	}

	if !gjson.Valid(document) {
		return false
	}

	for _, group := range f.clauses {
		if f.matchGroup(document, group) {
			return true
		}
	}

	return false
}

// matchGroup reports whether every clause of a conjunction group matches.
func (f *Filter) matchGroup(document string, group []clause) bool {
	for _, c := range group {
		if f.matchClause(document, c) == c.negated {
			return false
		}
	}

	return true
}

// matchClause evaluates a single comparison.
func (f *Filter) matchClause(document string, c clause) bool {
	if c.query != "" {
		// A gjson query selector: the path already carries the "#(...)".
		return gjson.Get(document, c.path+c.query).Exists()
	}

	value := gjson.Get(document, c.path)

	return compare(value, c.value, c.op)
}

// compare applies an operator to a JSON value.
//
// An array matches when any of its elements matches, which is the
// semantics the native grammar cannot express.
func compare(value gjson.Result, want, op string) bool {
	if value.IsArray() {
		matched := false

		value.ForEach(func(_, element gjson.Result) bool {
			if compare(element, want, op) {
				matched = true

				return false
			}

			return true
		})

		return matched
	}

	switch op {
	case "=":
		return value.String() == want
	case "!=":
		return value.String() != want
	case "~":
		return strings.Contains(value.String(), want)
	}

	got := value.Float()

	expected, err := strconv.ParseFloat(strings.TrimSpace(want), 64)
	if err != nil {
		return false
	}

	switch op {
	case ">":
		return got > expected
	case "<":
		return got < expected
	case ">=":
		return got >= expected
	case "<=":
		return got <= expected
	}

	return false
}

// parseClause parses a single comparison.
func parseClause(expr string) (clause, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return clause{}, fmt.Errorf("jsonfilter: the expression is empty")
	}

	// A negated clause: "not (path = value)".
	negated := false

	if strings.HasPrefix(expr, "not ") {
		negated = true
		expr = strings.TrimSpace(strings.TrimPrefix(expr, "not "))
	}

	// A parenthesised clause: "(path = value)".
	expr = stripOuterParens(expr)

	op, index := findOperator(expr)

	// A pure selector: "tags.#(==\"cli\")". The comparison lives inside
	// the selector, so there is no operator outside of it.
	if index < 0 {
		if i := strings.Index(expr, "#("); i >= 0 {
			return clause{path: expr[:i], query: expr[i:], negated: negated}, nil
		}

		return clause{}, fmt.Errorf("jsonfilter: %q has no comparison operator", expr)
	}

	path := strings.TrimSpace(expr[:index])
	value := strings.TrimSpace(expr[index+len(op):])

	if path == "" {
		return clause{}, fmt.Errorf("jsonfilter: %q has no left operand", expr)
	}

	c := clause{path: path, op: op, value: unquote(value), negated: negated}

	// A gjson query selector: "tags.#(==\"cli\")".
	if i := strings.Index(path, "#("); i >= 0 {
		c.path = path[:i]
		c.query = path[i:]
	}

	return c, nil
}

// operators are the comparison operators, from the longest to the
// shortest so that ">=" is not mistaken for ">".
var operators = []string{"!=", ">=", "<=", "=", ">", "<", "~"}

// findOperator returns the comparison operator of an expression and the
// index at which it starts.
//
// The operators that appear inside a quoted value or inside a "#(...)"
// selector are ignored, so that a selector like `tags.#(=="cli")` is not
// mistaken for an equality comparison.
func findOperator(expr string) (string, int) {
	inQuote := false
	inQuery := false

	for i := 0; i < len(expr); i++ {
		c := expr[i]

		switch {
		case inQuote:
			if c == '\\' {
				i++

				continue
			}

			if c == '"' {
				inQuote = false
			}

			continue
		case inQuery:
			if c == ')' {
				inQuery = false
			}

			continue
		case c == '"':
			inQuote = true

			continue
		}

		if c == '#' && i+1 < len(expr) && expr[i+1] == '(' {
			inQuery = true
			i++

			continue
		}

		for _, op := range operators {
			if !strings.HasPrefix(expr[i:], op) {
				continue
			}

			// The operator must be surrounded by spaces to be a
			// comparison and not part of a value.
			if i > 0 && expr[i-1] == ' ' &&
				i+len(op) < len(expr) && expr[i+len(op)] == ' ' {
				return op, i
			}
		}
	}

	return "", -1
}

// stripOuterParens removes the parentheses that wrap a whole expression.
func stripOuterParens(expr string) string {
	if len(expr) < 2 || expr[0] != '(' || expr[len(expr)-1] != ')' {
		return expr
	}

	depth := 0

	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '(':
			depth++
		case ')':
			depth--

			if depth == 0 && i != len(expr)-1 {
				return expr
			}
		}
	}

	return strings.TrimSpace(expr[1 : len(expr)-1])
}

// unquote removes the quotes of a string literal.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}

	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}

	return s
}

// splitTop splits an expression on a separator, ignoring the separators
// that appear inside a quoted value or inside a "#(...)" selector.
func splitTop(expr, separator string) ([]string, error) {
	var (
		parts   []string
		current strings.Builder
		inQuote bool
		inQuery bool
	)

	for i := 0; i < len(expr); i++ {
		c := expr[i]

		switch {
		case c == '"' && !inQuery:
			inQuote = !inQuote
		case c == '\'' && !inQuery:
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

	if inQuote {
		return nil, fmt.Errorf("jsonfilter: unterminated quote in %q", expr)
	}

	if inQuery {
		return nil, fmt.Errorf("jsonfilter: unterminated selector in %q", expr)
	}

	if strings.TrimSpace(current.String()) != "" {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	return parts, nil
}
