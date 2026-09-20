package livetest

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

func closeExporterSettles(t T, p Pair) { closeImplementation(t, p, false) }
func closeLocalSettles(t T, p Pair)    { closeImplementation(t, p, true) }

// A live scope ends callers without waiting for an implementation to cooperate.
// The peer stays open and the body is explicitly allowed to finish afterwards.
func closeImplementation(t T, p Pair, local bool) {
	t.Helper()
	entered, unblock, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	let := func() { once.Do(func() { close(unblock) }) }
	defer let()
	implementation := func(context.Context, json.RawMessage) (json.RawMessage, error) {
		close(entered)
		<-unblock
		close(finished)
		return json.RawMessage(`"late"`), nil
	}
	receiver := p.B
	if local {
		receiver = p.A
	}
	_, invoke := handed(t, p.A, receiver, sink, implementation)
	if err := p.A.Peer().Handle("ordinary", func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) { return raw, nil }); err != nil {
		t.Fatalf("ordinary handler: %v", err)
	}
	done := make(chan error, 1)
	go func() { _, err := invoke(context.Background(), nil); done <- err }()
	select {
	case <-entered:
	case <-time.After(waited):
		t.Fatalf("the invocation never entered")
	}
	if err := p.A.Close(); err != nil {
		t.Fatalf("closing exporter: %v", err)
	}
	select {
	case err := <-done:
		refused(t, err, live.ErrorScopeClosed)
	case <-time.After(waited):
		t.Fatalf("closure waited for the implementation to return")
	}
	holds(t, p.A, 0, 0, "closed exporter")
	ordinary := func() {
		c, cancel := ctx()
		defer cancel()
		var answer string
		if err := p.B.Peer().Call(c, "ordinary", "alive", &answer); err != nil || answer != "alive" {
			t.Fatalf("ordinary RPC after live close: %q, %v", answer, err)
		}
	}
	ordinary()
	let()
	select {
	case <-finished:
	case <-time.After(waited):
		t.Fatalf("the unblocked body never finished")
	}
	ordinary()
	_, err := invoke(context.Background(), nil)
	refused(t, err, live.ErrorScopeClosed)
}
