// Package diag holds what every layer reports problems with: the errors a
// caller can act on, and the list of warnings for problems that did not stop
// the work.
package diag

import (
	"errors"
	"fmt"
)

// ErrDamaged means the file is too damaged to read.
var ErrDamaged = errors.New("undocx: the document is damaged")

// Damaged returns an error that matches ErrDamaged and says what is wrong.
func Damaged(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrDamaged, fmt.Sprintf(format, args...))
}

// LimitError means the document exceeded a limit. What names the thing that
// was limited and Limit is the bound it ran into.
type LimitError struct {
	What  string
	Limit int64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("undocx: %s exceeds the limit of %d", e.What, e.Limit)
}

// Warning describes a problem that did not prevent reading the document.
type Warning struct {
	// Code identifies the kind of problem and is stable across releases.
	Code string
	// Message describes this instance of it.
	Message string
}

// WarnLimit is the code of the warning that says other warnings were dropped.
const WarnLimit = "limit"

// Log collects warnings, up to a maximum. The same warning is kept once.
type Log struct {
	max     int
	list    []Warning
	seen    map[Warning]bool
	dropped int
}

// NewLog returns a log that keeps at most max warnings.
func NewLog(max int) *Log {
	return &Log{max: max, seen: map[Warning]bool{}}
}

// Add records a warning. A nil log discards it.
func (l *Log) Add(code, format string, args ...any) {
	if l == nil {
		return
	}
	w := Warning{code, fmt.Sprintf(format, args...)}
	if l.seen[w] {
		return
	}
	if len(l.list) >= l.max {
		l.dropped++
		return
	}
	l.seen[w] = true
	l.list = append(l.list, w)
}

// List returns the warnings in the order they were added. When some were
// dropped, a last warning says how many.
func (l *Log) List() []Warning {
	if l == nil {
		return nil
	}
	out := append([]Warning(nil), l.list...)
	if l.dropped > 0 {
		out = append(out, Warning{WarnLimit, fmt.Sprintf("%d further warnings were dropped", l.dropped)})
	}
	return out
}
