package duplex_test

import (
	"errors"
	"reflect"
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

type declaredSend func([]string, bitwire.Message) error

func (f declaredSend) Send(p []string, m bitwire.Message) error { return f(p, m) }

func declaredNode(t *testing.T, origin bitwire.Wire, children ...duplex.DeclaredChild) duplex.Declared {
	t.Helper()
	n, err := duplex.ComposeDeclared(origin, children)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func declaredEvent() bitwire.Message {
	return bitwire.Message{Frame: bitwire.ProfileFrame{Version: 1, Kind: bitwire.ProfileEvent}}
}

func TestDeclaredDelegatesEveryFrameUnchangedToOriginAndCompleteChild(t *testing.T) {
	origin, child := &declaredOrigin{}, &declaredOrigin{}
	root := declaredNode(t, origin, duplex.DeclaredChild{Key: "a", Wire: child})
	access := root.Bind()
	capability := &bitwire.ReturnAddress{Wire: origin}
	for _, kind := range []bitwire.ProfileKind{bitwire.ProfileRequest, bitwire.ProfileEvent, bitwire.ProfileResponse, bitwire.ProfileCancel} {
		m := bitwire.Message{Frame: bitwire.ProfileFrame{Version: 1, Kind: kind, ID: "same", Data: []byte(`{"kept":true}`)}, Return: capability}
		for _, path := range [][]string{{}, {"a", "opaque", "suffix"}} {
			if err := access.Send(path, m); err != nil {
				t.Fatalf("%s: %v", kind, err)
			}
		}
		for _, got := range []bitwire.Message{origin.messages[len(origin.messages)-1], child.messages[len(child.messages)-1]} {
			if !reflect.DeepEqual(got, m) || got.Return != capability || &got.Frame.Data[0] != &m.Frame.Data[0] {
				t.Fatalf("%s: message or capability changed", kind)
			}
		}
	}
	if !reflect.DeepEqual(origin.paths, [][]string{{}, {}, {}, {}}) || !reflect.DeepEqual(child.paths, [][]string{{"opaque", "suffix"}, {"opaque", "suffix"}, {"opaque", "suffix"}, {"opaque", "suffix"}}) {
		t.Fatalf("wrong destinations: origin=%v child=%v", origin.paths, child.paths)
	}
	if err := access.Send([]string{"missing"}, declaredEvent()); !errors.Is(err, duplex.ErrNoRoute) {
		t.Fatal(err)
	}
	if len(origin.messages) != 4 || len(child.messages) != 4 {
		t.Fatal("missing child fell back to origin")
	}
}

func TestDeclaredRetainsOpaqueGuardAliasesAndStateAcrossReconstruction(t *testing.T) {
	target := &declaredOrigin{}
	remaining, checks := 2, 0
	refused := errors.New("guard exhausted")
	guard := &struct{ bitwire.Wire }{declaredSend(func(p []string, m bitwire.Message) error {
		checks++
		if remaining == 0 {
			return refused
		}
		remaining--
		return target.Send(p, m)
	})}
	root := declaredNode(t, duplex.RefusingOrigin{}, duplex.DeclaredChild{Key: "a", Wire: guard}, duplex.DeclaredChild{Key: "alias", Wire: guard})
	origin, children := root.Decompose()
	if children[0].Wire != guard || children[1].Wire != guard {
		t.Fatal("child identity lost")
	}
	rebuilt := declaredNode(t, origin, children...)
	for _, access := range []bitwire.Wire{duplex.At(root.Bind(), []string{"a"}), duplex.At(rebuilt.Bind(), []string{"alias"})} {
		if err := access.Send([]string{"tail"}, declaredEvent()); err != nil {
			t.Fatal(err)
		}
	}
	if err := rebuilt.Bind().Send([]string{"a"}, declaredEvent()); err != refused {
		t.Fatalf("lost refusal: %v", err)
	}
	if checks != 3 || len(target.messages) != 2 {
		t.Fatalf("guard reset, skipped or repeated: checks=%d deliveries=%d", checks, len(target.messages))
	}
}

func TestDeclaredCopiesPartsAndRetainsExactKeys(t *testing.T) {
	origin, child := &declaredOrigin{}, &declaredOrigin{}
	keys := []string{"\U00010000", "\ue000", "é", "e\u0301", "a/b", "", "\ufeff"}
	input := []duplex.DeclaredChild{}
	for _, key := range keys {
		input = append(input, duplex.DeclaredChild{Key: key, Wire: child})
	}
	root := declaredNode(t, origin, input...)
	input[0] = duplex.DeclaredChild{Key: "replaced", Wire: origin}
	gotOrigin, parts := root.Decompose()
	wantKeys := []string{"", "a/b", "e\u0301", "é", "\ue000", "\ufeff", "\U00010000"}
	if gotOrigin != origin || len(parts) != len(wantKeys) {
		t.Fatal("parts changed")
	}
	for i, part := range parts {
		if part.Key != wantKeys[i] || part.Wire != child {
			t.Fatalf("part %d = %#v", i, part)
		}
		if err := root.Bind().Send([]string{part.Key, "tail"}, declaredEvent()); err != nil {
			t.Fatal(err)
		}
	}
	parts[0] = duplex.DeclaredChild{Key: "mutated", Wire: origin}
	_, again := root.Decompose()
	if again[0].Key != "" || again[0].Wire != child {
		t.Fatal("decomposition leaked a mutable container")
	}
	for _, p := range child.paths {
		if !reflect.DeepEqual(p, []string{"tail"}) {
			t.Fatal(p)
		}
	}
	if err := root.Bind().Send([]string{"a", "b"}, declaredEvent()); !errors.Is(err, duplex.ErrNoRoute) {
		t.Fatal(err)
	}
}

func TestDeclaredCapturedAccessSurvivesAssemblerRebind(t *testing.T) {
	old, replacement := &declaredOrigin{}, &declaredOrigin{}
	branch := declaredNode(t, duplex.RefusingOrigin{}, duplex.DeclaredChild{Key: "run", Wire: duplex.At(old, []string{"original"})})
	root := declaredNode(t, duplex.RefusingOrigin{}, duplex.DeclaredChild{Key: "svc", Wire: branch.Bind()})
	captured := duplex.At(duplex.At(root.Bind(), []string{"svc"}), []string{"run"})
	origin, children := root.Decompose()
	rebuilt := declaredNode(t, origin, children...)
	root = declaredNode(t, replacement)
	for _, access := range []bitwire.Wire{captured, duplex.At(rebuilt.Bind(), []string{"svc", "run"})} {
		if err := access.Send([]string{"tail"}, declaredEvent()); err != nil {
			t.Fatal(err)
		}
	}
	if err := root.Bind().Send(nil, declaredEvent()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old.paths, [][]string{{"original", "tail"}, {"original", "tail"}}) || len(replacement.messages) != 1 {
		t.Fatal("old access was rebound")
	}
}

func TestDeclaredValidationAndPrivateDelegationPath(t *testing.T) {
	valid := &declaredOrigin{}
	var missingPointer *declaredOrigin
	var missingFunction declaredSend
	for _, missing := range []bitwire.Wire{nil, missingPointer, missingFunction} {
		if _, err := duplex.ComposeDeclared(missing, nil); !errors.Is(err, duplex.ErrDeclaredValue) {
			t.Fatalf("missing origin: %v", err)
		}
		if _, err := duplex.ComposeDeclared(valid, []duplex.DeclaredChild{{Key: "hole", Wire: missing}}); !errors.Is(err, duplex.ErrDeclaredValue) {
			t.Fatalf("missing child: %v", err)
		}
	}
	if _, err := duplex.ComposeDeclared(valid, []duplex.DeclaredChild{{Key: "x", Wire: valid}, {Key: "x", Wire: valid}}); !errors.Is(err, duplex.ErrDeclaredChildExists) {
		t.Fatal(err)
	}
	invalid := string([]byte{0xff})
	if _, err := duplex.ComposeDeclared(valid, []duplex.DeclaredChild{{Key: invalid, Wire: valid}}); !errors.Is(err, duplex.ErrPath) {
		t.Fatal(err)
	}
	mutator := declaredSend(func(p []string, _ bitwire.Message) error { p[0] = "changed"; return nil })
	root := declaredNode(t, valid, duplex.DeclaredChild{Key: "x", Wire: mutator})
	path := []string{"x", "tail"}
	if err := root.Bind().Send(path, declaredEvent()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(path, []string{"x", "tail"}) {
		t.Fatal("child changed caller path")
	}
	if err := root.Bind().Send([]string{"x", invalid}, declaredEvent()); !errors.Is(err, duplex.ErrPath) {
		t.Fatal(err)
	}
	var zero duplex.Declared
	if err := zero.Bind().Send(nil, declaredEvent()); !errors.Is(err, duplex.ErrDeclaredValue) {
		t.Fatal(err)
	}
}

type declaredEndpoint struct {
	declaredOrigin
	received, closed bool
}

func (e *declaredEndpoint) Receive(bitwire.Receiver) (func(), error) {
	e.received = true
	return func() {}, nil
}
func (e *declaredEndpoint) Close(bitwire.Code, string) error { e.closed = true; return nil }

func TestDeclaredAccessDoesNotGrantPartsOrBorrowLifecycle(t *testing.T) {
	endpoint := &declaredEndpoint{}
	root := declaredNode(t, endpoint, duplex.DeclaredChild{Key: "child", Wire: endpoint})
	access := root.Bind()
	if _, ok := access.(bitwire.Endpoint); ok {
		t.Fatal("access grants lifecycle")
	}
	if _, ok := access.(interface {
		Decompose() (bitwire.Wire, []duplex.DeclaredChild)
	}); ok {
		t.Fatal("access grants parts")
	}
	if _, ok := any(root).(bitwire.Wire); ok {
		t.Fatal("description is access")
	}
	origin, parts := root.Decompose()
	rebuilt := declaredNode(t, origin, parts...)
	for _, wire := range []bitwire.Wire{access, rebuilt.Bind(), endpoint} {
		if err := wire.Send(nil, declaredEvent()); err != nil {
			t.Fatal(err)
		}
	}
	if endpoint.received || endpoint.closed {
		t.Fatal("composition acquired endpoint ownership")
	}
}
