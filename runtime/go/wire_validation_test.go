package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	bitwire "github.com/Bitspark/bitwire/wire/go"
	"strings"
	"testing"

	ws "github.com/Bitspark/nightseam/runtime/go"
)

func TestWireRefusesMalformedFramesBeforeDispatch(t *testing.T) {
	client, server := newPair(t, ws.Options{}, ws.Options{MaxFrameBytes: 256})
	invoked := 0
	serverBinding := testBinding(t, server.Wire())
	_, err := ws.HandleWire(serverBinding, []string{"echo"}, func(_ context.Context, raw json.RawMessage) (any, error) {
		invoked++
		return raw, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sink := &wireReplySink{replies: make(chan bitwire.ProfileFrame, 30)}
	address := &bitwire.ReturnAddress{Wire: sink}
	valid := bitwire.ProfileFrame{Version: 1, Kind: bitwire.ProfileRequest, ID: "c:1", Params: json.RawMessage("{}")}
	for name, change := range map[string]func(*bitwire.ProfileFrame){
		"version":           func(f *bitwire.ProfileFrame) { f.Version = 0 },
		"id":                func(f *bitwire.ProfileFrame) { f.ID = "unscoped" },
		"zero id":           func(f *bitwire.ProfileFrame) { f.ID = "c:0" },
		"missing params":    func(f *bitwire.ProfileFrame) { f.Params = nil },
		"foreign member":    func(f *bitwire.ProfileFrame) { f.Result = json.RawMessage("null") },
		"trace":             func(f *bitwire.ProfileFrame) { f.Traceparent = "invalid" },
		"reserved metadata": func(f *bitwire.ProfileFrame) { f.Meta = map[string]string{"nightseam.future": "value"} },
		"oversize":          func(f *bitwire.ProfileFrame) { f.Params, _ = json.Marshal(strings.Repeat("x", 300)) },
	} {
		t.Run(name, func(t *testing.T) {
			frame := valid
			change(&frame)
			if err := client.Wire().Send([]string{"echo"}, bitwire.Message{Frame: frame, Return: address}); err == nil {
				t.Errorf("malformed frame admitted")
			}
		})
	}
	var result string
	if err := ws.CallWire(context.Background(), client.Wire(), []string{"echo"}, "still usable", &result); err != nil || result != "still usable" {
		t.Fatalf("healthy call = %q, %v", result, err)
	}
	if invoked != 1 {
		t.Fatalf("handler invoked %d times, want only the valid call", invoked)
	}
}

func TestWireSanitizesMalformedPublicErrors(t *testing.T) {
	client, server := newPair(t, ws.Options{}, ws.Options{})
	serverBinding := testBinding(t, server.Wire())
	for _, value := range []*ws.PublicError{nil, {Code: ""}, {Code: "bad", Message: ""}} {
		detach, err := ws.HandleWire(serverBinding, []string{"fail"}, func(context.Context, json.RawMessage) (any, error) { return nil, value })
		if err != nil {
			t.Fatal(err)
		}
		err = ws.CallWire(context.Background(), client.Wire(), []string{"fail"}, nil, nil)
		var public *ws.PublicError
		if !errors.As(err, &public) || public == nil || public.Code != "internal" {
			t.Errorf("malformed public error = %v", err)
		}
		detach()
	}
}
