package composition_test

import (
	"context"
	"sync"
	"testing"

	jobprotocol "example.test/generated/api/go/job-protocol"
	sinkprotocol "example.test/generated/api/go/sink-protocol"
)

// An ending is visible to the callback before its acknowledgement reaches
// the worker. Hold that interval open without relying on scheduler timing.
type heldEnding struct {
	*recorder
	entered chan struct{}
	release chan struct{}
}

func (s *heldEnding) End(ctx context.Context, ending sinkprotocol.Ending) (int64, error) {
	taken, err := s.recorder.End(ctx, ending)
	close(s.entered)
	select {
	case <-s.release:
		return taken, err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func TestCompletionWaitsForFinalCallbackAcknowledgement(t *testing.T) {
	ctx := testContext(t)
	sink := &heldEnding{newRecorder(), make(chan struct{}), make(chan struct{})}
	release := sync.OnceFunc(func() { close(sink.release) })
	defer release()
	job := &theJob{label: "held-ending", steps: 1, sink: sink, cancel: make(chan struct{}), done: make(chan struct{})}
	go job.run(ctx)
	select {
	case <-sink.entered:
	case <-ctx.Done():
		t.Fatalf("waiting for final callback: %v", ctx.Err())
	}
	_, _, ended, endings := sink.snapshot()
	status, err := job.Status(ctx)
	if err != nil || status.State != "done" || status.Delivered != 1 || ended != "done" || endings != 1 {
		t.Fatalf("unacknowledged ending: status=%#v err=%v ending=%q count=%d", status, err, ended, endings)
	}
	select {
	case <-job.done:
		t.Fatal("worker finished before the final callback was acknowledged")
	default:
	}
	type answer struct {
		value jobprotocol.Cancelled
		err   error
	}
	answered := make(chan answer, 1)
	go func() {
		value, err := job.Cancel(ctx)
		answered <- answer{value, err}
	}()
	select {
	case <-job.cancel:
	case <-ctx.Done():
		t.Fatalf("waiting for cancellation during final callback: %v", ctx.Err())
	}
	release()
	select {
	case got := <-answered:
		if got.err != nil || got.value.Stopped || got.value.Delivered != 1 {
			t.Fatalf("cancellation during final acknowledgement: %#v, %v", got.value, got.err)
		}
	case <-ctx.Done():
		t.Fatalf("waiting for cancellation answer: %v", ctx.Err())
	}
	select {
	case <-job.done:
	case <-ctx.Done():
		t.Fatalf("waiting for worker completion: %v", ctx.Err())
	}
	if _, err := job.Cancel(ctx); !jobprotocol.IsError(err, jobprotocol.ErrorJobFinished) {
		t.Fatalf("cancelling a finished job answered %v", err)
	}
}
