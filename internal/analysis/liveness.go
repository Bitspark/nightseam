package analysis

import (
	"sort"

	"github.com/Bitspark/nightseam/internal/model"
)

// Liveness is the one property the live tier adds to the type language: a
// value is **live** when it carries a callable, and a callable is the one
// kind whose values are not self-contained data. Nothing declares it — it
// is derived, so that a declaration cannot be live and say otherwise, and
// so that a family that gains a callable in one record does not have to
// re-annotate everything that reaches it.
//
// It is the least fixed point of "contains a callable" over the
// declaration graph. A type reached again while it is being decided
// contributes nothing on that edge, which is what makes a recursive live
// record terminate and what keeps a recursive data record data.
//
// Two edges are deliberately not followed:
//
//   - `{"ref": "E"}` is a reference to an entity by its key, which is
//     data. A live entity has a live value and a data key, and the two
//     identities stay apart ([#201]'s verdict, [#196]'s distinction).
//   - A type parameter is live exactly when what fills it is, so liveness
//     of an `apply` is decided under that application's bindings and an
//     unfilled parameter contributes nothing. `Page<Job>` is live and
//     `Page<Payload>` is not, from one declaration of `Page`.
//
// A draw through a family parameter, `S.T`, is live exactly when the
// parameter is of the live tier: a family that carries the live tier may
// draw a live type through it, and one of the protocol tier may not.
type liveness struct {
	deciding map[*model.Type]bool
	decided  map[*model.Type]bool
}

func newLiveness() *liveness {
	return &liveness{deciding: map[*model.Type]bool{}, decided: map[*model.Type]bool{}}
}

// IsLive reports whether values of an expression carry a callable, in this
// family's scope and with no parameter bound.
func (f *Family) IsLive(e model.TypeExpr) bool {
	return newLiveness().expr(f, e, nil)
}

// IsLiveType reports whether values of a declared type of this family
// carry a callable. An unknown name is not live.
func (f *Family) IsLiveType(name string) bool {
	t, ok := f.Types[name]
	if !ok {
		return false
	}
	return newLiveness().typ(f, t, nil)
}

// LiveTypes is every declared type of this family whose values carry a
// callable, in byte order: what a target renders boundary conversion for.
func (f *Family) LiveTypes() []string {
	l := newLiveness()
	var names []string
	for name, t := range f.Types {
		if f.IsCarried(name) {
			continue
		}
		if l.typ(f, t, nil) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Callables is every callable this family declares, in byte order.
func (f *Family) Callables() []string {
	var names []string
	for name, t := range f.Types {
		if !f.IsCarried(name) && t.IsCallable() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// bindings says, for each parameter of the declaration being decided,
// whether what fills it is live. A nil map is a declaration decided with
// nothing bound.
type bindings map[string]bool

func (l *liveness) expr(f *Family, e model.TypeExpr, bound bindings) bool {
	switch x := e.(type) {
	case nil:
		return false
	case model.Primitive, model.Literal:
		return false
	case model.Ref:
		// A key, not a binding: the whole point of keeping the two apart.
		return false
	case model.Named:
		if live, isParameter := bound[x.Name]; isParameter {
			return live
		}
		if t, ok := f.Types[x.Name]; ok {
			return l.typ(f, t, nil)
		}
		return false
	case model.Imported:
		other, ok := f.Imported[x.Family]
		if !ok {
			return false
		}
		t, ok := other.Types[x.Name]
		if !ok {
			return false
		}
		return l.typ(other, t, nil)
	case model.Drawn:
		parameter, ok := f.Parameter(x.Parameter)
		return ok && parameter.Of == model.LiveRole
	case model.Array:
		return l.expr(f, x.Elem, bound)
	case model.Map:
		return l.expr(f, x.Elem, bound)
	case model.Nullable:
		return l.expr(f, x.Elem, bound)
	case model.Inline:
		return l.typ(f, x.Type, bound)
	case model.Apply:
		return l.apply(f, x, bound)
	}
	return false
}

// apply decides the applied declaration under this edge's bindings: each
// of its parameters is live exactly when what fills it here is.
func (l *liveness) apply(f *Family, x model.Apply, bound bindings) bool {
	owner := f
	if x.Family != "" {
		other, ok := f.Imported[x.Family]
		if !ok {
			return false
		}
		owner = other
	}
	t, ok := owner.Types[x.Name]
	if !ok {
		return false
	}
	inner := bindings{}
	for parameter, filler := range x.With {
		if filler.Family != "" {
			// A family fills a family parameter; a draw through it is
			// decided by the tier the parameter is of, not here.
			continue
		}
		inner[parameter] = l.expr(f, filler.Type, bound)
	}
	return l.typ(owner, t, inner)
}

func (l *liveness) typ(f *Family, t *model.Type, bound bindings) bool {
	if t == nil {
		return false
	}
	// Only a decision reached with nothing bound is worth keeping: an
	// applied view is decided per edge.
	cacheable := len(bound) == 0
	if cacheable {
		if live, ok := l.decided[t]; ok {
			return live
		}
		if l.deciding[t] {
			// Reached again while being decided: this edge adds nothing,
			// which is what terminates a recursive declaration.
			return false
		}
		l.deciding[t] = true
		defer delete(l.deciding, t)
	}
	live := l.body(f, t, bound)
	if cacheable {
		l.decided[t] = live
	}
	return live
}

func (l *liveness) body(f *Family, t *model.Type, bound bindings) bool {
	if t.IsCallable() {
		return true
	}
	for _, base := range t.Extends {
		if l.expr(f, base.Expression(), bound) {
			return true
		}
	}
	switch t.Kind {
	case model.KindRecord, model.KindEntity:
		for i := range t.Fields {
			if l.expr(f, t.Fields[i].Type, bound) {
				return true
			}
		}
	case model.KindUnion:
		for i := range t.Variants {
			if l.expr(f, t.Variants[i].Type, bound) {
				return true
			}
		}
	case model.KindAlias:
		return l.expr(f, t.Alias, bound)
	}
	return false
}
