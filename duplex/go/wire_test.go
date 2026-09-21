package duplex_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/Bitspark/nightseam/duplex/go"
)

func TestPathEncodingIsCanonicalAndComposable(t *testing.T) {
	paths := [][]string{{}, {""}, {"a", "b"}, {"a.b"}, {"a", "b:c"}, {"é", "e\u0301", "😀", "\ufeff", "\x00"}}
	seen := map[string]bool{}
	for _, path := range paths {
		encoded, err := duplex.EncodePath(path)
		if err != nil {
			t.Fatal(err)
		}
		if seen[encoded] {
			t.Fatalf("paths alias at %q", encoded)
		}
		seen[encoded] = true
		decoded, err := duplex.DecodePath(encoded)
		if err != nil || !reflect.DeepEqual(decoded, path) {
			t.Fatalf("%q: %#v, %v", encoded, decoded, err)
		}
		for _, suffix := range paths {
			b, _ := duplex.EncodePath(suffix)
			combined, _ := duplex.EncodePath(append(append([]string{}, path...), suffix...))
			if combined != encoded+b {
				t.Fatal("prefixing did not compose by concatenation")
			}
		}
	}
	if got, _ := duplex.EncodePath([]string{"a", "😀", ""}); got != "1:a4:😀0:" {
		t.Fatal(got)
	}
	for _, malformed := range []string{"01:a", "00:", "1", ":", "-1:a", "2:a", "1:é", "99999999999999999999999999999:x", "1:\xff"} {
		if _, err := duplex.DecodePath(malformed); err == nil {
			t.Fatalf("accepted %q", malformed)
		}
	}
	if _, err := duplex.EncodePath([]string{"\xff"}); err == nil {
		t.Fatal("accepted non-scalar UTF-8")
	}
}

// queuedRoot is a deterministic dispatch fixture, not a transport. Only drain
// executes destination callbacks, so a composition cannot pass by timing luck.
type queuedRoot struct {
	mu        sync.Mutex
	queue     []queuedDelivery
	receivers map[string]duplex.Receiver
	closed    bool
	closes    int
}
type queuedDelivery struct {
	path    []string
	message duplex.Message
}

// Wire values need not be comparable; only ReturnAddress identity is compared.
type nonComparableRoot struct {
	*queuedRoot
	marker []int
}

func newRoot() *queuedRoot { return &queuedRoot{receivers: map[string]duplex.Receiver{}} }
func (r *queuedRoot) Send(path []string, message duplex.Message) error {
	key, err := duplex.EncodePath(path)
	if err != nil {
		return err
	}
	_ = key
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return duplex.ErrClosed
	}
	r.queue = append(r.queue, queuedDelivery{append([]string{}, path...), message})
	return nil
}
func (r *queuedRoot) Receive(path []string, receiver duplex.Receiver) (func(), error) {
	key, err := duplex.EncodePath(path)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, duplex.ErrClosed
	}
	if _, exists := r.receivers[key]; exists {
		return nil, duplex.ErrReceiverExists
	}
	r.receivers[key] = receiver
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.receivers, key); r.mu.Unlock() }) }, nil
}
func (r *queuedRoot) Close(code duplex.Code, reason string) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.closes++
	receivers := r.receivers
	r.receivers = map[string]duplex.Receiver{}
	r.mu.Unlock()
	for _, receiver := range receivers {
		if receiver.Closed != nil {
			receiver.Closed(code, reason)
		}
	}
	return nil
}
func (r *queuedRoot) drain() {
	for {
		r.mu.Lock()
		if len(r.queue) == 0 {
			r.mu.Unlock()
			return
		}
		d := r.queue[0]
		r.queue = r.queue[1:]
		key, _ := duplex.EncodePath(d.path)
		receiver := r.receivers[key]
		r.mu.Unlock()
		if receiver.Message != nil {
			receiver.Message([]string{}, d.message)
		}
	}
}

func TestWireSelectionMountAndReturnCapability(t *testing.T) {
	root, reply := newRoot(), newRoot()
	address := &duplex.ReturnAddress{Wire: nonComparableRoot{reply, []int{1}}}
	prefix := []string{"a.b"}
	selected := duplex.At(root, prefix)
	prefix[0] = "changed"
	children := map[string]duplex.Wire{"x": selected}
	mounted := duplex.Mount(children)
	children["x"] = reply
	view := duplex.At(duplex.At(mounted, []string{"x"}), []string{"😀"})
	var received []duplex.Message
	detach, err := view.Receive([]string{"call"}, duplex.Receiver{Message: func(path []string, m duplex.Message) {
		if len(path) != 0 {
			t.Errorf("receiver path = %v", path)
		}
		received = append(received, m)
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	frames := []duplex.ProfileFrame{
		{Version: 1, Kind: duplex.ProfileRequest, ID: "c:1", Params: json.RawMessage(`{"n":9007199254740993}`), Meta: map[string]string{"tag": "value"}},
		{Version: 1, Kind: duplex.ProfileResponse, ID: "c:1", Error: &duplex.ProfileError{Code: "refused", Message: "No", Data: json.RawMessage(`{"why":"test"}`)}},
		{Version: 1, Kind: duplex.ProfileEvent, Data: json.RawMessage(`null`)},
		{Version: 1, Kind: duplex.ProfileCancel, ID: "c:1"},
	}
	for _, frame := range frames {
		message := duplex.Message{Frame: frame, Return: address}
		if err := view.Send([]string{"call"}, message); err != nil {
			t.Fatal(err)
		}
	}
	if len(received) != 0 {
		t.Fatal("destination ran inside Send")
	}
	if len(root.queue) != 4 || len(reply.queue) != 0 {
		t.Fatal("composition did not delegate to the existing root")
	}
	for _, d := range root.queue {
		if !reflect.DeepEqual(d.path, []string{"a.b", "😀", "call"}) {
			t.Fatal(d.path)
		}
	}
	root.drain()
	if len(received) != 4 {
		t.Fatal(received)
	}
	for i, frame := range frames {
		if !reflect.DeepEqual(received[i].Frame, frame) || received[i].Return != address {
			t.Fatal(received[i])
		}
	}
	if _, err := duplex.At(root, []string{"a.b", "😀"}).Receive([]string{"call"}, duplex.Receiver{}); !errors.Is(err, duplex.ErrReceiverExists) {
		t.Fatalf("duplicate registration: %v", err)
	}
	detach()
	if detach, err := duplex.At(root, nil).Receive([]string{"a.b", "😀", "call"}, duplex.Receiver{}); err != nil {
		t.Fatal(err)
	} else {
		detach()
	}
}

func TestMountCloseDetachesOnlyItsRegistrationsAndPreservesChildren(t *testing.T) {
	root := newRoot()
	mounted := duplex.Mount(map[string]duplex.Wire{"": root})
	selected := duplex.At(mounted, []string{""})
	closed := 0
	_, err := selected.Receive([]string{"call"}, duplex.Receiver{Closed: func(code duplex.Code, reason string) {
		closed++
		if code != duplex.CodeNormal || reason != "mount ended" {
			t.Error(code, reason)
		}
		_ = mounted.Close(code, reason) // Reentrant close must not hold its mutex.
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mounted.Receive(nil, duplex.Receiver{}); !errors.Is(err, duplex.ErrNoRoute) {
		t.Fatal(err)
	}
	if err := mounted.Close(duplex.CodeNormal, "mount ended"); err != nil {
		t.Fatal(err)
	}
	_ = mounted.Close(duplex.CodeNormal, "again")
	if closed != 1 || root.closes != 0 || len(root.receivers) != 0 {
		t.Fatalf("close=%d child=%d registrations=%d", closed, root.closes, len(root.receivers))
	}
	if err := selected.Send([]string{"call"}, duplex.Message{}); !errors.Is(err, duplex.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := selected.Receive([]string{"call"}, duplex.Receiver{}); !errors.Is(err, duplex.ErrClosed) {
		t.Fatal(err)
	}
	if err := root.Send([]string{"call"}, duplex.Message{}); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Receive([]string{"call"}, duplex.Receiver{}); err != nil {
		t.Fatal(err)
	}
	if err := duplex.At(root, []string{"call"}).Close(duplex.CodeNormal, "root ended"); err != nil || root.closes != 1 {
		t.Fatal(err, root.closes)
	}
}

type registeringRoot struct {
	*queuedRoot
	registered chan struct{}
	resume     chan struct{}
}

func (r *registeringRoot) Receive(path []string, receiver duplex.Receiver) (func(), error) {
	detach, err := r.queuedRoot.Receive(path, receiver)
	close(r.registered)
	<-r.resume
	return detach, err
}

func TestMountCloseDuringReceiveCannotLeaveAChildRegistration(t *testing.T) {
	root := &registeringRoot{newRoot(), make(chan struct{}), make(chan struct{})}
	mounted := duplex.Mount(map[string]duplex.Wire{"x": root})
	finished := make(chan error, 1)
	closed := 0
	go func() {
		_, err := mounted.Receive([]string{"x", "call"}, duplex.Receiver{Closed: func(duplex.Code, string) { closed++ }})
		finished <- err
	}()
	<-root.registered
	_ = mounted.Close(duplex.CodeNormal, "done")
	close(root.resume)
	if err := <-finished; !errors.Is(err, duplex.ErrClosed) {
		t.Fatal(err)
	}
	if len(root.receivers) != 0 || root.closes != 0 || closed != 1 {
		t.Fatal(len(root.receivers), root.closes, closed)
	}
}

func TestMountedChildCloseDoesNotEndItsSibling(t *testing.T) {
	left, right := newRoot(), newRoot()
	mounted := duplex.Mount(map[string]duplex.Wire{"left": left, "right": right})
	closed := 0
	_, err := mounted.Receive([]string{"left", "call"}, duplex.Receiver{Closed: func(duplex.Code, string) { closed++ }})
	if err != nil {
		t.Fatal(err)
	}
	_ = left.Close(duplex.CodeNormal, "child ended")
	if err := mounted.Send([]string{"right", "call"}, duplex.Message{}); err != nil {
		t.Fatal(err)
	}
	_ = mounted.Close(duplex.CodeNormal, "mount ended")
	if closed != 1 || right.closes != 0 || len(right.queue) != 1 {
		t.Fatal(closed, right.closes, len(right.queue))
	}
}
