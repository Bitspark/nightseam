package runtime_test

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Bitspark/nightseam/duplex/go"
	ws "github.com/Bitspark/nightseam/runtime/go"
)

func TestInvocationRetirementNeedsBodyAndControlDrain(t *testing.T) {
	for _, late := range []bool{false, true} {
		var retired atomic.Int32
		owner, routing, execution, err := ws.NewInvocation(ws.InvocationLimits{Captures: 3, Bodies: 1}, func() { retired.Add(1) })
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := routing.(ws.InvocationOwner); ok {
			t.Fatal("routing grants owner authority")
		}
		if _, ok := routing.(ws.InvocationExecution); ok {
			t.Fatal("routing grants execution authority")
		}
		body, err := execution.Begin()
		if err != nil {
			t.Fatal(err)
		}
		entered, release, drained := make(chan struct{}), make(chan struct{}), make(chan struct{})
		capture, err := routing.Capture(func(duplex.Message) { close(entered); <-release })
		if err != nil {
			t.Fatal(err)
		}
		control, err := owner.QueueControl(duplex.Message{Frame: duplex.ProfileFrame{Kind: duplex.ProfileCancel}})
		if err != nil {
			t.Fatal(err)
		}
		if late {
			control.Deliver()
			go func() { capture.Delivered(); close(drained) }()
		} else {
			capture.Delivered()
			go func() { control.Deliver(); close(drained) }()
		}
		<-entered
		owner.DispatchDone()
		owner.DispatchDone()
		owner.Settle()
		body.Done()
		body.Done()
		if retired.Load() != 0 {
			t.Fatal("retired while cancellation callback is running")
		}
		close(release)
		<-drained
		if retired.Load() != 1 {
			t.Fatal("did not retire exactly once after control drain")
		}
		control.Deliver()
		capture.Delivered()
		if _, err := routing.Capture(nil); !errors.Is(err, ws.ErrInvocationEnded) {
			t.Fatal(err)
		}
		if _, err := execution.Begin(); !errors.Is(err, ws.ErrInvocationEnded) {
			t.Fatal(err)
		}
	}
}

func TestInvocationBoundsAndReentrantCancellation(t *testing.T) {
	retired := 0
	owner, routing, execution, err := ws.NewInvocation(ws.InvocationLimits{Captures: 2, Bodies: 1}, func() { retired++ })
	if err != nil {
		t.Fatal(err)
	}
	body, _ := execution.Begin()
	if _, err := execution.Begin(); !errors.Is(err, ws.ErrInvocationLimit) {
		t.Fatal(err)
	}
	var ticket ws.InvocationControl
	seen := 0
	first, _ := routing.Capture(func(duplex.Message) { seen++; ticket.Deliver() })
	first.Delivered()
	ticket, err = owner.QueueControl(duplex.Message{Frame: duplex.ProfileFrame{Kind: duplex.ProfileCancel}})
	if err != nil {
		t.Fatal(err)
	}
	ticket.Deliver()
	second, err := routing.Capture(func(message duplex.Message) {
		seen++
		if next, err := owner.QueueControl(message); next != nil || err != nil {
			t.Errorf("control was not coalesced: %v", err)
		}
		owner.Settle()
		body.Done()
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := routing.Capture(nil); !errors.Is(err, ws.ErrInvocationLimit) {
		t.Fatal(err)
	}
	owner.DispatchDone()
	second.Delivered()
	if seen != 2 || retired != 1 {
		t.Fatalf("controls=%d retired=%d", seen, retired)
	}
}

func TestInvocationSequentialCompletionDoesNotRetainCapacity(t *testing.T) {
	retired := 0
	var retained []ws.InvocationRouting
	for range 256 {
		owner, routing, execution, err := ws.NewInvocation(ws.InvocationLimits{Captures: 1, Bodies: 1}, func() { retired++ })
		if err != nil {
			t.Fatal(err)
		}
		capture, _ := routing.Capture(func(duplex.Message) { t.Error("completed invocation received control") })
		body, _ := execution.Begin()
		capture.Delivered()
		owner.DispatchDone()
		owner.Settle()
		if retired != len(retained) {
			t.Fatal("outcome released unfinished body")
		}
		body.Done()
		retained = append(retained, routing)
	}
	if retired != len(retained) {
		t.Fatal("completed invocations retained admission")
	}
}
