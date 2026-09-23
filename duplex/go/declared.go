package duplex

import (
	"errors"
	"reflect"
	"slices"

	bitwire "github.com/Bitspark/bitwire/wire/go"
)

var (
	ErrDeclaredValue       = errors.New("declared composition requires an origin and complete child access")
	ErrDeclaredChildExists = errors.New("declared child key already exists")
)

// RefusingOrigin is explicit origin behavior for a node which only groups children.
type RefusingOrigin struct{}

func (RefusingOrigin) Send([]string, bitwire.Message) error { return ErrNoRoute }

// DeclaredChild retains complete send access: an endpoint, selected view, forwarder,
// guard or another bound composite. Its internal structure need not be declared.
type DeclaredChild struct {
	Key  string
	Wire bitwire.Wire
}

// Declared is an immutable construction description owned by an assembler.
// Origin and child capabilities are borrowed, retaining their state and identities.
// Bind returns separate send-only access; callers cannot recover these parts from it.
// Keep descriptions separately to reconstruct a recursively declared tree. The zero
// value is not an admitted description.
type Declared struct{ node *declaredNode }

type declaredNode struct {
	origin   bitwire.Wire
	children map[string]bitwire.Wire
}

// ComposeDeclared copies a complete sequence of child entries, refusing duplicates
// before a native map can overwrite them. Keys are exact Unicode scalar strings;
// empty keys are allowed. Origin handles only [], never missing-child fallback.
// Nil origins or children (including typed nils) are refused. Construction acquires
// no receiver attachments, queues, peers, invocation state or lifecycle authority.
func ComposeDeclared(origin bitwire.Wire, children []DeclaredChild) (Declared, error) {
	if missingDeclaredWire(origin) {
		return Declared{}, ErrDeclaredValue
	}
	routes := make(map[string]bitwire.Wire, len(children))
	for _, child := range children {
		if _, err := EncodePath([]string{child.Key}); err != nil {
			return Declared{}, err
		}
		if missingDeclaredWire(child.Wire) {
			return Declared{}, ErrDeclaredValue
		}
		if _, found := routes[child.Key]; found {
			return Declared{}, ErrDeclaredChildExists
		}
		routes[child.Key] = child.Wire
	}
	return Declared{node: &declaredNode{origin: origin, children: routes}}, nil
}

func missingDeclaredWire(w bitwire.Wire) bool {
	if w == nil {
		return true
	}
	v := reflect.ValueOf(w)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// Decompose returns the original origin and complete child access, ordered by exact
// UTF-8 key bytes. Only the containers are copied; modifying the returned slice
// cannot change the description. Opaque children are never inspected or unwrapped.
func (d Declared) Decompose() (bitwire.Wire, []DeclaredChild) {
	if d.node == nil {
		return nil, nil
	}
	keys := make([]string, 0, len(d.node.children))
	for key := range d.node.children {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	children := make([]DeclaredChild, 0, len(keys))
	for _, key := range keys {
		children = append(children, DeclaredChild{key, d.node.children[key]})
	}
	return d.node.origin, children
}

// Bind grants only send access to this immutable description. Each send delegates
// the unchanged message once, to the origin at [] or to the named child with one
// segment removed. Every frame kind follows the same rule. Validation, admission,
// asynchronous dispatch and invocation lifetime belong to the destination/profile;
// guards are ordinary Wire wrappers. Existing access stays bound after an assembler
// rebuilds or replaces its description. There is nothing to attach or close here.
func (d Declared) Bind() bitwire.Wire { return &declaredWire{node: d.node} }

type declaredWire struct{ node *declaredNode }

func (w *declaredWire) Send(path []string, message bitwire.Message) error {
	if w.node == nil {
		return ErrDeclaredValue
	}
	if _, err := EncodePath(path); err != nil {
		return err
	}
	if len(path) == 0 {
		return w.node.origin.Send([]string{}, message)
	}
	child, found := w.node.children[path[0]]
	if !found {
		return ErrNoRoute
	}
	return child.Send(append([]string{}, path[1:]...), message)
}
