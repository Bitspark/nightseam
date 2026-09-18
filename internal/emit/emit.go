// Package emit is what every target writes source with: a writer that
// keeps the indentation, an import set an emitter declares at the site
// that needs it, and a namespace that holds the identifiers a rendering
// declares so that a collision is found before a line is written.
package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Bitspark/nightseam/internal/diag"
)

// Writer builds a source file line by line, indenting what is inside a
// block.
type Writer struct {
	b      strings.Builder
	depth  int
	indent string
}

// NewWriter makes a writer indenting with the given string per level.
func NewWriter(indent string) *Writer { return &Writer{indent: indent} }

// Line writes one line at the current depth; an empty line stays empty.
func (w *Writer) Line(text string) {
	if text != "" {
		w.b.WriteString(strings.Repeat(w.indent, w.depth))
		w.b.WriteString(text)
	}
	w.b.WriteByte('\n')
}

// Linef writes one formatted line.
func (w *Writer) Linef(format string, args ...any) { w.Line(fmt.Sprintf(format, args...)) }

// Raw writes text as it is, with no indentation.
func (w *Writer) Raw(text string) { w.b.WriteString(text) }

// In deepens the indentation; Out shallows it.
func (w *Writer) In()  { w.depth++ }
func (w *Writer) Out() { w.depth-- }

// Block writes an opening line, the body one level deeper, and a closing
// line.
func (w *Writer) Block(open, close string, body func()) {
	w.Line(open)
	w.In()
	body()
	w.Out()
	w.Line(close)
}

// String is the source so far.
func (w *Writer) String() string { return w.b.String() }

// Imports is the set of imports a file declares: each an alias and a path,
// registered where an emitter uses it.
type Imports struct {
	paths map[string]string
}

// Import is one import.
type Import struct{ Alias, Path string }

// Use registers an import and returns its alias, for the emitter to spell.
func (i *Imports) Use(alias, path string) string {
	if i.paths == nil {
		i.paths = map[string]string{}
	}
	i.paths[alias] = path
	return alias
}

// Sorted is every import, by alias.
func (i *Imports) Sorted() []Import {
	out := make([]Import, 0, len(i.paths))
	for alias, path := range i.paths {
		out = append(out, Import{alias, path})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Alias < out[b].Alias })
	return out
}

// Namespace holds the identifiers one scope of a rendering declares: what
// the generated code declares of itself, fixed first, and what the family
// makes it declare, each checked against the others as it is declared.
type Namespace struct {
	Name    string
	members map[string]string // identifier → what declares it
}

// NewNamespace makes a namespace of the given name, for diagnostics.
func NewNamespace(name string) *Namespace {
	return &Namespace{Name: name, members: map[string]string{}}
}

// Fix declares identifiers the generated code declares of itself, which a
// family may not declare.
func (n *Namespace) Fix(origin string, idents ...string) {
	for _, ident := range idents {
		n.members[ident] = origin
	}
}

// Reserved reports what fixed the identifier, if the generated code did.
func (n *Namespace) Reserved(ident string) (string, bool) {
	origin, ok := n.members[ident]
	return origin, ok
}

// Declare declares an identifier for a declaration of the family, located
// for a diagnostic: nil when it is free, reserved_name when the generated
// code declares it, generated_name_collision when another declaration of
// the family does.
func (n *Namespace) Declare(family string, ident string, at diag.Location, what string) *diag.Diagnostic {
	if previous, taken := n.members[ident]; taken {
		code := "generated_name_collision"
		if strings.HasPrefix(previous, "generated ") {
			code = "reserved_name"
		}
		d := diag.New(family, at, code, fmt.Sprintf("%s %s collides with the %s in the %s.", what, ident, previous, n.Name))
		return &d
	}
	n.members[ident] = what + " " + ident
	return nil
}

// Members is every identifier declared, sorted, with what declares it.
func (n *Namespace) Members() []string {
	out := make([]string, 0, len(n.members))
	for ident, origin := range n.members {
		out = append(out, ident+"\t"+origin)
	}
	sort.Strings(out)
	return out
}
