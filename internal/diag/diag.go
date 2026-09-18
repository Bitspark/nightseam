// Package diag is the vocabulary every other package reports problems in: a
// diagnostic locates one problem in a family by the tier file and the JSON
// pointer of what is wrong, names it by a code, and says what to write
// instead. It imports nothing of the generator.
package diag

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Location is where a declaration sits: a tier file of a family and a JSON
// pointer into it. The loader stamps one on every declaration it decodes;
// a check derives the pointer of a part of it with Sub.
type Location struct {
	File    string
	Pointer string
}

// Sub is the location of a part of this one: each segment appended to the
// pointer, escaped, an integer as an index.
func (l Location) Sub(segments ...any) Location {
	var b strings.Builder
	b.WriteString(l.Pointer)
	for _, segment := range segments {
		b.WriteByte('/')
		switch s := segment.(type) {
		case int:
			b.WriteString(strconv.Itoa(s))
		case string:
			b.WriteString(Escape(s))
		default:
			b.WriteString(Escape(fmt.Sprint(s)))
		}
	}
	return Location{File: l.File, Pointer: b.String()}
}

// String spells a location as file#pointer.
func (l Location) String() string {
	if l.File == "" {
		return l.Pointer
	}
	return l.File + "#" + l.Pointer
}

// Diagnostic is one problem: the family it is in, where, what code names
// it, and what the message says to do about it.
type Diagnostic struct {
	Family  string
	File    string
	Pointer string
	Code    string
	Message string
}

// New makes a diagnostic at a location.
func New(family string, at Location, code, message string) Diagnostic {
	return Diagnostic{Family: family, File: at.File, Pointer: at.Pointer, Code: code, Message: message}
}

// At is the diagnostic's location.
func (d Diagnostic) At() Location { return Location{File: d.File, Pointer: d.Pointer} }

// String is the one line the tool prints: family/file#pointer: message [code].
func (d Diagnostic) String() string {
	where := d.At().String()
	if d.Family != "" {
		where = d.Family + "/" + where
	}
	return fmt.Sprintf("%s: %s [%s]", where, d.Message, d.Code)
}

// Escape escapes one JSON-pointer segment.
func Escape(segment string) string {
	return strings.ReplaceAll(strings.ReplaceAll(segment, "~", "~0"), "/", "~1")
}

// Sort orders diagnostics by family, file, pointer, code and message, so
// that a merged set from several checks reads the same every time.
func Sort(diagnostics []Diagnostic) {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		a, b := diagnostics[i], diagnostics[j]
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Pointer != b.Pointer {
			return a.Pointer < b.Pointer
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}

// List collects diagnostics for one family.
type List struct {
	Family      string
	Diagnostics []Diagnostic
}

// Add appends one diagnostic at a location.
func (l *List) Add(at Location, code, message string) {
	l.Diagnostics = append(l.Diagnostics, New(l.Family, at, code, message))
}

// Addf appends one diagnostic with a formatted message.
func (l *List) Addf(at Location, code, format string, args ...any) {
	l.Add(at, code, fmt.Sprintf(format, args...))
}
