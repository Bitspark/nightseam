package runtime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	ws "github.com/Bitspark/nightseam/runtime/go"
)

// A full destination's consumer remains held while the caller finishes. The
// write deadline is deliberately much longer than the caller's bound: waiting
// for that deadline would make fan-out run at its slowest destination's pace.
func TestWireSendEndsAFullCarrierWithoutWaitingForItsConsumer(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	control := &delayedWriteControl{started: make(chan struct{}), gate: release}
	observed := &recorder{}
	_, destination := delayedWritePair(t, control, ws.Options{
		QueueCapacity: 1,
		WriteTimeout:  5 * time.Second,
		Observer:      observed,
	}, ws.Options{})
	if err := destination.Emit(context.Background(), "first", 1); err != nil {
		t.Fatal(err)
	}
	receive(t, control.started)
	if err := destination.Emit(context.Background(), "second", 2); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- destination.Emit(context.Background(), "overflow", 3) }()
	select {
	case err := <-finished:
		if !errors.Is(err, ws.ErrBackpressure) {
			t.Fatalf("overflow = %v, want backpressure", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("send waited for a destination whose consumer is still held")
	}
	if !errors.Is(destination.Err(), ws.ErrBackpressure) {
		t.Fatalf("full carrier remained open: %v", destination.Err())
	}
	stalls := 0
	for _, event := range observed.all() {
		if pressure, ok := event.(ws.Backpressure); ok && pressure.Stalled {
			stalls++
		}
	}
	if stalls != 1 {
		t.Fatalf("observer received %d terminal pressure events, want 1", stalls)
	}
}
