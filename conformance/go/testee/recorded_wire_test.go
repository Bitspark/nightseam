package main

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
)

func TestRecordedRootDeliversRootRelativePath(t *testing.T) {
	root := newRecordedRoot()
	defer root.Close(duplex.CodeNormal, "done")
	delivered := make(chan []string, 1)
	want := []string{"source", "tick"}
	_, err := root.Receive(duplex.Receiver{Message: func(path []string, _ duplex.Message) { delivered <- path }})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Send(want, recordedMessage(1)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := recordedWait(ctx, delivered)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delivered path %q; want %q", got, want)
	}
}

// The shared scenario, not a second language-specific oracle, owns the verdict.
func TestRecordedWireHeadAndOrder(t *testing.T) {
	data, err := os.ReadFile("../../scenarios/peer/recorded-wire-head-and-order.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenario struct{ Steps []struct{ Expect any } }
	if err := json.Unmarshal(data, &scenario); err != nil {
		t.Fatal(err)
	}
	answer := newTestee().serve([]byte(`{"id":1,"op":"peer.recorded_wire_witness","within_ms":2000}`))
	if answer.Error != nil {
		t.Fatal(answer.Error)
	}
	encoded, err := json.Marshal(answer.OK)
	if err != nil {
		t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, scenario.Steps[0].Expect) {
		t.Fatalf("got %s; want %#v", encoded, scenario.Steps[0].Expect)
	}
}
