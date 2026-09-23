package duplex_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	bitwire "github.com/Bitspark/bitwire/wire/go"
	"github.com/Bitspark/nightseam/duplex/go"
)

type declaredOrigin struct {
	paths    [][]string
	messages []bitwire.Message
}

func (o *declaredOrigin) Send(path []string, message bitwire.Message) error {
	o.paths = append(o.paths, append([]string{}, path...))
	o.messages = append(o.messages, message)
	return nil
}

type declaredPolicy struct {
	mu        sync.Mutex
	remaining int
	paths     [][]string
}

var errDeclaredQuota = errors.New("quota exhausted")

func (p *declaredPolicy) Admit(path []string, _ bitwire.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paths = append(p.paths, append([]string{}, path...))
	if p.remaining == 0 {
		return errDeclaredQuota
	}
	if p.remaining > 0 {
		p.remaining--
	}
	return nil
}

func declaredNode(t *testing.T, own bitwire.Wire, policy duplex.AdmissionPolicy, children ...duplex.DeclaredChild) duplex.Declared {
	t.Helper()
	n, err := duplex.ComposeDeclared(duplex.DeclaredValue{Own: own, Policy: policy}, children)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func declaredEvent() bitwire.Message {
	return bitwire.Message{Frame: bitwire.ProfileFrame{Version: 1, Kind: bitwire.ProfileEvent}}
}

func TestDeclaredOwnOriginAndChildrenRemainIndependent(t *testing.T) {
	parent, child := &declaredOrigin{}, &declaredOrigin{}
	root := declaredNode(t, parent, duplex.PermitAdmission{})
	leaf := declaredNode(t, child, duplex.PermitAdmission{})
	grown, err := root.Attach(nil, "", leaf)
	if err != nil {
		t.Fatal(err)
	}
	message := declaredEvent()
	message.Return = &bitwire.ReturnAddress{Wire: parent}
	for _, path := range [][]string{{}, {""}} {
		if err := grown.Bind().Send(path, message); err != nil {
			t.Fatal(err)
		}
	}
	if len(parent.messages) != 1 || len(child.messages) != 1 || parent.messages[0].Return != message.Return || child.messages[0].Return != message.Return {
		t.Fatal("own access, child access or original return identity was lost")
	}
	if !reflect.DeepEqual(parent.paths, [][]string{{}}) || !reflect.DeepEqual(child.paths, [][]string{{}}) {
		t.Fatal("an origin received a nonempty suffix")
	}
	if err := grown.Bind().Send([]string{"missing"}, message); !errors.Is(err, duplex.ErrNoRoute) {
		t.Fatalf("missing child: %v", err)
	}
	if len(parent.messages) != 1 {
		t.Fatal("missing child fell back to own access")
	}
	if _, ok := root.At([]string{""}); ok {
		t.Fatal("attachment mutated the previous declaration")
	}
	value, _ := grown.Decompose()
	if value.Own != parent {
		t.Fatal("attachment replaced the parent's own value")
	}
	if _, err := grown.Attach(nil, "", leaf); !errors.Is(err, duplex.ErrDeclaredChildExists) {
		t.Fatalf("occupied key: %v", err)
	}
	if _, err := root.Attach([]string{"absent"}, "x", leaf); !errors.Is(err, duplex.ErrNoRoute) {
		t.Fatalf("missing parent: %v", err)
	}
}

func TestDeclaredPartsRetainRawChildrenAndSharedPolicies(t *testing.T) {
	origin := &declaredOrigin{}
	gate := &declaredPolicy{remaining: 2}
	leaf := declaredNode(t, origin, duplex.PermitAdmission{})
	entries := []duplex.DeclaredChild{{Key: "x", Node: leaf}, {Key: "y", Node: leaf}}
	root := declaredNode(t, duplex.RefusingOrigin{}, gate, entries...)
	entries[0].Node = duplex.Declared{}
	value, children := root.Decompose()
	if value.Policy != gate || len(children) != 2 || children[0].Node != leaf || children[1].Node != leaf {
		t.Fatal("parts changed identity or aliases")
	}
	rebuilt, err := duplex.ComposeDeclared(value, children)
	if err != nil {
		t.Fatal(err)
	}
	children[0].Node = duplex.Declared{}
	if err := duplex.At(root.Bind(), []string{"x"}).Send(nil, declaredEvent()); err != nil {
		t.Fatal(err)
	}
	if err := duplex.At(rebuilt.Bind(), []string{"y"}).Send(nil, declaredEvent()); err != nil {
		t.Fatal(err)
	}
	if err := rebuilt.Bind().Send([]string{"x"}, declaredEvent()); !errors.Is(err, errDeclaredQuota) {
		t.Fatalf("reconstruction reset or bypassed quota: %v", err)
	}
	if !reflect.DeepEqual(gate.paths, [][]string{{"x"}, {"y"}, {"x"}}) || len(origin.messages) != 2 {
		t.Fatal("ancestor checks were repeated, bypassed or rebased incorrectly")
	}
	if _, ok := root.Bind().(bitwire.Endpoint); ok {
		t.Fatal("bound access grants endpoint ownership")
	}
}

func TestDeclaredPolicyOccurrencesAndRefusedDestinations(t *testing.T) {
	gate := &declaredPolicy{remaining: -1}
	origin := &declaredOrigin{}
	leaf := declaredNode(t, origin, gate)
	root := declaredNode(t, duplex.RefusingOrigin{}, gate, duplex.DeclaredChild{Key: "x", Node: leaf})
	if err := root.Bind().Send([]string{"x"}, declaredEvent()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gate.paths, [][]string{{"x"}, {}}) {
		t.Fatal("shared policy was deduplicated instead of checked once per occurrence")
	}
	if err := root.Bind().Send([]string{"absent"}, declaredEvent()); !errors.Is(err, duplex.ErrNoRoute) {
		t.Fatal(err)
	}
	if len(gate.paths) != 3 {
		t.Fatal("policy did not precede child lookup")
	}
	for _, kind := range []bitwire.ProfileKind{bitwire.ProfileResponse, bitwire.ProfileCancel} {
		message := declaredEvent()
		message.Frame.Kind = kind
		if err := root.Bind().Send([]string{"x"}, message); !errors.Is(err, duplex.ErrDeclaredFrame) {
			t.Fatalf("control entered admission: %v", err)
		}
	}
	if len(gate.paths) != 3 {
		t.Fatal("a control consumed admission state")
	}
}

func TestDeclaredConstructionAndNavigationRefuseInvalidParts(t *testing.T) {
	origin := &declaredOrigin{}
	leaf := declaredNode(t, origin, duplex.PermitAdmission{})
	value := duplex.DeclaredValue{Own: origin, Policy: duplex.PermitAdmission{}}
	for _, children := range [][]duplex.DeclaredChild{
		{{Key: "x", Node: leaf}, {Key: "x", Node: leaf}},
		{{Key: "x", Node: duplex.Declared{}}},
		{{Key: string([]byte{0xff}), Node: leaf}},
	} {
		if _, err := duplex.ComposeDeclared(value, children); err == nil {
			t.Fatal("invalid declaration accepted")
		}
	}
	for _, value := range []duplex.DeclaredValue{{Policy: duplex.PermitAdmission{}}, {Own: origin}} {
		if _, err := duplex.ComposeDeclared(value, nil); err == nil {
			t.Fatal("missing own access or explicit policy accepted")
		}
	}
	root := declaredNode(t, origin, duplex.PermitAdmission{},
		duplex.DeclaredChild{Key: "a/b", Node: leaf}, duplex.DeclaredChild{Key: "é", Node: leaf}, duplex.DeclaredChild{Key: "e\u0301", Node: leaf})
	if _, ok := root.At([]string{"a", "b"}); ok {
		t.Fatal("navigation split an opaque segment")
	}
	for _, key := range []string{"a/b", "é", "e\u0301"} {
		if _, ok := root.At([]string{key}); !ok {
			t.Fatalf("lost key %q", key)
		}
	}
	if err := root.Bind().Send([]string{string([]byte{0xff})}, declaredEvent()); !errors.Is(err, duplex.ErrPath) {
		t.Fatal(err)
	}
}

func TestDeclaredRebuiltViewsShareConcurrentAdmissionState(t *testing.T) {
	gate := &declaredPolicy{remaining: 100}
	root := declaredNode(t, duplex.RefusingOrigin{}, gate)
	value, children := root.Decompose()
	rebuilt, err := duplex.ComposeDeclared(value, children)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 200; i++ {
		workers.Go(func() { _ = rebuilt.Bind().Send(nil, declaredEvent()) })
	}
	workers.Wait()
	if gate.remaining != 0 || len(gate.paths) != 200 {
		t.Fatal("reconstructed views did not share the synchronized policy")
	}
}

func TestDeclaredReplacingAssemblerHandlesCannotRetargetBoundAccess(t *testing.T) {
	origin, replacement := &declaredOrigin{}, &declaredOrigin{}
	leaf := declaredNode(t, origin, duplex.PermitAdmission{})
	root := declaredNode(t, origin, duplex.PermitAdmission{}, duplex.DeclaredChild{Key: "child", Node: leaf})
	bound := root.Bind()
	selected := duplex.At(bound, []string{"child"})
	leaf = declaredNode(t, replacement, duplex.PermitAdmission{})
	root = leaf
	if err := bound.Send(nil, declaredEvent()); err != nil {
		t.Fatal(err)
	}
	if err := selected.Send(nil, declaredEvent()); err != nil {
		t.Fatal(err)
	}
	if len(origin.messages) != 2 || len(replacement.messages) != 0 {
		t.Fatal("replacing assembler handles retargeted previously bound access")
	}
}
