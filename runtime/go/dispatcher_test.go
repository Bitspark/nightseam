package runtime_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Bitspark/nightseam/duplex/go"
	ws "github.com/Bitspark/nightseam/runtime/go"
)

func TestDispatcherSharesOneAttachmentAndPreservesBorrowedEndpoint(t *testing.T) {
	left, right, err := ws.NewWirePair(ws.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = left.Close(duplex.CodeNormal, "") })
	dispatch, err := ws.NewDispatcher(right)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := right.Receive(duplex.Receiver{}); err == nil {
		t.Fatal("second owning attachment accepted")
	}
	for _, name := range []string{"a", "b"} {
		view := dispatch.Select([]string{name})
		binding, err := ws.NewDispatcher(view)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ws.HandleWire(binding, []string{"read"}, func(context.Context, json.RawMessage) (any, error) { return name, nil }); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a", "b"} {
		var got string
		if err := ws.CallWire(context.Background(), left, []string{name, "read"}, nil, &got); err != nil || got != name {
			t.Fatalf("%s: %q, %v", name, got, err)
		}
	}
	if err := dispatch.Close(duplex.CodeNormal, ""); err != nil {
		t.Fatal(err)
	}
	rebound, err := ws.NewDispatcher(right)
	if err != nil {
		t.Fatalf("borrowed endpoint was closed: %v", err)
	}
	if _, err := ws.HandleWire(rebound, []string{"read"}, func(context.Context, json.RawMessage) (any, error) { return "rebound", nil }); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := ws.CallWire(context.Background(), left, []string{"read"}, nil, &got); err != nil || got != "rebound" {
		t.Fatalf("rebound: %q, %v", got, err)
	}
}

func TestDispatcherClosesAnExplicitlyOwnedEndpoint(t *testing.T) {
	left, right, err := ws.NewWirePair(ws.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = left.Close(duplex.CodeNormal, "") })
	dispatch, err := ws.NewDispatcher(right, ws.DispatcherOptions{OwnEndpoint: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Close(duplex.CodeProtocolError, "wire event rejected"); err != nil {
		t.Fatal(err)
	}
	if _, err := right.Receive(duplex.Receiver{}); err == nil {
		t.Fatal("owned endpoint stayed open")
	}
	if err := dispatch.Close(duplex.CodeNormal, "again"); err != nil {
		t.Fatal(err)
	}
}

func TestDispatcherRefusesUnmanagedInvocationWithoutReplacingReturn(t *testing.T) {
	root := &unmanagedDispatchEndpoint{}
	dispatch, err := ws.NewDispatcher(root)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if _, err := dispatch.Register(nil, duplex.Receiver{Message: func([]string, duplex.Message) { called = true }}); err != nil {
		t.Fatal(err)
	}
	replies := &wireReplySink{replies: make(chan duplex.ProfileFrame, 1)}
	address := &duplex.ReturnAddress{Wire: replies}
	root.receiver.Message(nil, duplex.Message{Frame: duplex.ProfileFrame{Version: 1, Kind: duplex.ProfileRequest, ID: "one", Params: json.RawMessage(`null`)}, Return: address})
	if called {
		t.Fatal("unmanaged invocation dispatched without a lifetime association")
	}
	if reply := receive(t, replies.replies); reply.Error == nil || reply.Error.Code != "invalid_message" {
		t.Fatalf("unmanaged refusal = %+v", reply)
	}
}

type unmanagedDispatchEndpoint struct{ receiver duplex.Receiver }

func (*unmanagedDispatchEndpoint) Send([]string, duplex.Message) error { return nil }
func (w *unmanagedDispatchEndpoint) Receive(receiver duplex.Receiver) (func(), error) {
	w.receiver = receiver
	return func() {}, nil
}
func (*unmanagedDispatchEndpoint) Close(duplex.Code, string) error { return nil }
