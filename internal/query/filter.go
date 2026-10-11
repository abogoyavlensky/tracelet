package query

import "strings"

// filter accumulates a WHERE clause and its arguments.
type filter struct {
	where []string
	args  []any
}

func (f *filter) add(cond string, a ...any) {
	f.where = append(f.where, cond)
	f.args = append(f.args, a...)
}

// clause is the conditions joined with AND.
func (f *filter) clause() string { return strings.Join(f.where, " AND ") }

// attrPath is the JSON path of a top-level key, quoted so a dotted OTLP name
// such as exception.type matches the key rather than a nested path.
func attrPath(key string) string {
	key = strings.ReplaceAll(key, `\`, `\\`)
	key = strings.ReplaceAll(key, `"`, `\"`)
	return `$."` + key + `"`
}
