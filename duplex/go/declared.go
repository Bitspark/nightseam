package duplex

import (
	"errors"
	"slices"

	bitwire "github.com/Bitspark/bitwire/wire/go"
)

var (
	ErrDeclaredValue       = errors.New("declared composition requires own access, policy and valid children")
	ErrDeclaredChildExists = errors.New("declared child key already exists")
	ErrDeclaredFrame       = errors.New("declared admission accepts only requests and events")
)

// AdmissionPolicy checks one occurrence before origin dispatch or child lookup.
// It must be bounded, synchronous and nonblocking, and must not mutate the message.
// Mutable policies synchronize their own state, including state shared by different
// declarations. Refusal ends traversal; an earlier admission is not rolled back.
// This is an admission check, not an application handler or completion permit.
type AdmissionPolicy interface {
	Admit(path []string, message bitwire.Message) error
}

// AdmissionFunc adapts a synchronous admission check. Captured state is retained
// across reconstruction; a concurrently used check must synchronize that state.
type AdmissionFunc func([]string, bitwire.Message) error

func (f AdmissionFunc) Admit(path []string, message bitwire.Message) error {
	if f == nil {
		return ErrDeclaredValue
	}
	return f(path, message)
}

// PermitAdmission is the explicit identity policy.
type PermitAdmission struct{}

func (PermitAdmission) Admit([]string, bitwire.Message) error { return nil }

// RefusingOrigin is an explicit own value for a node which only groups children.
type RefusingOrigin struct{}

func (RefusingOrigin) Send([]string, bitwire.Message) error { return ErrNoRoute }

// DeclaredValue is the own value of Bitwire ADR0005's structural interpretation.
// Own receives only the empty relative path; it is never a missing-child fallback.
// Policy guards this node and every descendant reached through this occurrence.
// Both are borrowed and retained unchanged. Neither may be a nil interface.
type DeclaredValue struct {
	Own    bitwire.Wire
	Policy AdmissionPolicy
}

// DeclaredChild retains a whole raw construction description, not a selected view.
type DeclaredChild struct {
	Key  string
	Node Declared
}

// Declared is an immutable, finite construction description owned by an assembler.
// Its own values may retain live state. Sharing a child or policy shares that state.
// It is not itself Wire access: Bind returns a separate send-only facade.
// Descriptions expose raw parts and therefore must not be given to callers who
// should receive only guarded access. The zero value is not an admitted declaration.
// Copying or replacing a value handle cannot alter a retained declaration.
type Declared struct {
	node *declaredNode
}

type declaredNode struct {
	value    DeclaredValue
	children map[string]Declared
}

// ComposeDeclared retains the own value and copies a complete child map. A sequence
// of entries is accepted so duplicate keys are refused before a native map can
// overwrite them. Keys must be Unicode scalar strings; empty keys are allowed.
// There are no receiver attachments, peers, queues or lifecycle acquisitions.
func ComposeDeclared(value DeclaredValue, children []DeclaredChild) (Declared, error) {
	if value.Own == nil || value.Policy == nil {
		return Declared{}, ErrDeclaredValue
	}
	routes := make(map[string]Declared, len(children))
	for _, child := range children {
		if _, err := EncodePath([]string{child.Key}); err != nil {
			return Declared{}, err
		}
		if !child.Node.valid() {
			return Declared{}, ErrDeclaredValue
		}
		if _, found := routes[child.Key]; found {
			return Declared{}, ErrDeclaredChildExists
		}
		routes[child.Key] = child.Node
	}
	return Declared{node: &declaredNode{value: value, children: routes}}, nil
}

func (d Declared) valid() bool { return d.node != nil }

// Decompose returns the own value and all raw children. Capability, policy and child
// identities are retained; changing the returned slice cannot change the declaration.
// The slice is ordered by exact UTF-8 key bytes. Only the construction owner has parts;
// selected Wire access neither exposes them nor drops its inherited admission checks.
func (d Declared) Decompose() (DeclaredValue, []DeclaredChild) {
	if !d.valid() {
		return DeclaredValue{}, nil
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
	return d.node.value, children
}

// At resolves raw construction parts for the assembler. It is not a guarded Wire
// selection. Use At(d.Bind(), path) to give a caller access with ancestor policies.
func (d Declared) At(path []string) (Declared, bool) {
	if !d.valid() {
		return Declared{}, false
	}
	if _, err := EncodePath(path); err != nil {
		return Declared{}, false
	}
	current := d
	for _, key := range path {
		current = current.node.children[key]
		if !current.valid() {
			return Declared{}, false
		}
	}
	return current, true
}

// Attach constructs an exact fresh-child extension. The parent must exist, the
// key must be unused, and child must be valid. Every old own value, policy and
// untouched child is retained. Existing views keep the old structure and work.
// No application effect or provider edit is executed by structural attachment.
func (d Declared) Attach(parent []string, key string, child Declared) (Declared, error) {
	if _, err := EncodePath(append(append([]string{}, parent...), key)); err != nil {
		return Declared{}, err
	}
	if !child.valid() {
		return Declared{}, ErrDeclaredValue
	}
	target, found := d.At(parent)
	if !found {
		return Declared{}, ErrNoRoute
	}
	if _, occupied := target.node.children[key]; occupied {
		return Declared{}, ErrDeclaredChildExists
	}
	return d.attach(parent, key, child), nil
}

func (d Declared) attach(parent []string, key string, child Declared) Declared {
	value, children := d.Decompose()
	if len(parent) == 0 {
		children = append(children, DeclaredChild{key, child})
	} else {
		for i := range children {
			if children[i].Key == parent[0] {
				children[i].Node = children[i].Node.attach(parent[1:], key, child)
				break
			}
		}
	}
	// The public entry validated the path, all nodes and freshness.
	result, _ := ComposeDeclared(value, children)
	return result
}

// Bind returns only send access and grants no construction, Receive or Close
// authority. Selection through At keeps this declaration and ancestor policies.
// Rebuilding from selected children is not decomposition: it repeats their guards.
// Responses and cancellation use the captured invocation's facilities, not this
// new-admission entry. Destination endpoints remain responsible for asynchronous
// application dispatch, admission validation and invocation lifetime.
func (d Declared) Bind() bitwire.Wire { return &declaredWire{root: d} }

type declaredWire struct{ root Declared }

func (w *declaredWire) Send(path []string, message bitwire.Message) error {
	if _, err := EncodePath(path); err != nil {
		return err
	}
	if message.Frame.Kind != bitwire.ProfileRequest && message.Frame.Kind != bitwire.ProfileEvent {
		return ErrDeclaredFrame
	}
	if !w.root.valid() {
		return ErrDeclaredValue
	}
	current := w.root
	for i := 0; ; i++ {
		// A check sees a private path slice; it cannot retarget later traversal.
		if err := current.node.value.Policy.Admit(append([]string{}, path[i:]...), message); err != nil {
			return err
		}
		if i == len(path) {
			return current.node.value.Own.Send([]string{}, message)
		}
		current = current.node.children[path[i]]
		if !current.valid() {
			return ErrNoRoute
		}
	}
}
