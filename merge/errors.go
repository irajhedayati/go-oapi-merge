package merge

import "strings"

// MergeError describes a failure encountered while resolving a $ref during
// the merge. It carries the offending file, the JSON pointer path within
// that file (when known), a human-readable message, and an optional
// underlying cause that can be retrieved with errors.Unwrap.
type MergeError struct {
	File    string
	Path    string
	Message string
	Cause   error
}

func (e *MergeError) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	if e.File != "" {
		b.WriteString(" (in ")
		b.WriteString(e.File)
		if e.Path != "" {
			b.WriteString(" at ")
			b.WriteString(e.Path)
		}
		b.WriteString(")")
	}
	return b.String()
}

func (e *MergeError) Unwrap() error {
	return e.Cause
}
