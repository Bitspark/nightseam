package livetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

func publicationCases() []Case {
	return []Case{
		{"retainedAfterTimeout", retainedAfterTimeout},
		{"retainedAfterCancellation", retainedAfterCancellation},
		{"retainedAfterRemoteError", retainedAfterRemoteError},
		{"retainedAfterLostReply", retainedAfterLostReply},
		{"remoteInvokesAfterFailedSupply", remoteInvokesAfterFailedSupply},
		{"handlerOwnerAfterLostReply", handlerOwnerAfterLostReply},
		{"ownerReleaseWhileRemoteAliasExists", ownerReleaseWhileRemoteAliasExists},
		{"repeatedFailuresDoNotLeak", repeatedFailuresDoNotLeak},
		{"eventPublicationRetainsItsOwner", eventPublicationRetainsItsOwner},
		{"scopeClosureEndsRetainedOwners", scopeClosureEndsRetainedOwners},
		{"provenUnpublishedIsUnwound", provenUnpublishedIsUnwound},
	}
}

func publicationAwait[V any](t T, ch <-chan V, what string) V {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(waited):
		t.Fatalf("%s never completed", what)
		var zero V
		return zero
	}
}

func publicationCounts(t T, owner *live.Owner, exports, imports int, where string) {
	t.Helper()
	if got := owner.Counts(); got != (live.Counts{Exports: exports, Imports: imports}) {
		t.Errorf("%s: owner holds %+v, want %d exports and %d imports", where, got, exports, imports)
	}
}

func publicationWaitCounts(t T, scope *live.Scope, exports, imports int) {
	t.Helper()
	deadline := time.NewTimer(waited)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	want := live.Counts{Exports: exports, Imports: imports}
	for scope.Counts() != want {
		select {
		case <-deadline.C:
			t.Fatalf("release was not delivered: scope holds %+v, want %+v", scope.Counts(), want)
			return
		case <-tick.C:
		}
	}
}

func publicationPayload(t T, owner *live.Owner) json.RawMessage {
	t.Helper()
	raw, err := owner.ExportValue(func(build *live.Owner) (json.RawMessage, error) {
		reference, err := build.Export(sink, echo)
		if err != nil {
			return nil, err
		}
		return json.Marshal(reference)
	})
	if err != nil {
		t.Fatalf("constructing supplied callback: %v", err)
	}
	return raw
}

func publicationImport(owner *live.Owner, raw json.RawMessage) (live.Invoke, error) {
	reference, err := owner.Scope().Decode(raw)
	if err != nil {
		return nil, err
	}
	return owner.Import(reference, sink)
}

func publicationAlive(t T, p Pair) {
	t.Helper()
	c, cancel := ctx()
	defer cancel()
	var value int
	if err := p.A.Peer().Call(c, "publication.alive", 17, &value); err != nil || value != 17 {
		t.Fatalf("ordinary RPC after failed publication: %d, %v", value, err)
	}
}

func publicationInstallAlive(t T, p Pair) {
	t.Helper()
	if err := p.B.Peer().Handle("publication.alive", func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
		return raw, nil
	}); err != nil {
		t.Fatalf("installing ordinary RPC: %v", err)
	}
}

func retainedAfterTimeout(t T, p Pair)               { failedSupply(t, p, "timeout", 1) }
func retainedAfterCancellation(t T, p Pair)          { failedSupply(t, p, "cancellation", 1) }
func retainedAfterRemoteError(t T, p Pair)           { failedSupply(t, p, "busy", 1) }
func retainedAfterLostReply(t T, p Pair)             { failedSupply(t, p, "lost", 1) }
func remoteInvokesAfterFailedSupply(t T, p Pair)     { failedSupply(t, p, "frame_too_large", 1) }
func ownerReleaseWhileRemoteAliasExists(t T, p Pair) { failedSupply(t, p, "refused", 1) }
func repeatedFailuresDoNotLeak(t T, p Pair)          { failedSupply(t, p, "refused", 12) }

// The supplying RPC actually imports the callback before it fails. Reusing
// the payload across failed supplies must borrow the same attachment, and no
// error code -- even one also used by a local send refusal -- proves rollback.
func failedSupply(t T, p Pair, outcome string, repetitions int) {
	t.Helper()
	publicationInstallAlive(t, p)
	outgoing, incoming := p.A.Owner().Child(), p.B.Owner().Child()
	defer outgoing.Release()
	defer incoming.Release()
	raw := publicationPayload(t, outgoing)
	retained := make(chan live.Invoke, repetitions)
	unblock := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(unblock) }) }
	defer finish()
	if err := p.B.Peer().Handle("publication.supply", func(c context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
		invoke, err := publicationImport(incoming, raw)
		if err != nil {
			return nil, err
		}
		retained <- invoke
		switch outcome {
		case "timeout", "cancellation":
			<-c.Done()
			return nil, c.Err()
		case "lost":
			// Ignore cancellation: the retained value must survive even when
			// this late reply cannot settle the original caller.
			<-unblock
			return "late", nil
		default:
			return nil, &runtime.PublicError{Code: outcome, Message: "retained before refusing"}
		}
	}); err != nil {
		t.Fatalf("installing supply: %v", err)
	}
	var alias live.Invoke
	for i := 0; i < repetitions; i++ {
		c, cancel := context.WithCancel(context.Background())
		if outcome == "timeout" || outcome == "lost" {
			cancel()
			c, cancel = context.WithTimeout(context.Background(), time.Second)
		}
		done := make(chan error, 1)
		go func() { done <- p.A.Peer().Call(c, "publication.supply", raw, nil) }()
		alias = publicationAwait(t, retained, "the remote retaining the callback")
		if outcome == "cancellation" {
			cancel()
		}
		err := publicationAwait(t, done, "the failed supplying RPC")
		cancel()
		switch outcome {
		case "timeout", "lost":
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("supplying deadline: %v", err)
			}
		case "cancellation":
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("supplying cancellation: %v", err)
			}
		default:
			refused(t, err, outcome)
		}
		publicationCounts(t, outgoing, 1, 0, "failed supplier's reachable owner")
		publicationCounts(t, incoming, 0, 1, "retaining owner")
		holds(t, p.A, 1, 0, "supplying scope")
		holds(t, p.B, 0, 1, "receiving scope")
		if got := string(call(t, alias, "41")); got != "41" {
			t.Fatalf("retained callback after %s: %s", outcome, got)
		}
		publicationAlive(t, p)
	}
	finish()
	if err := outgoing.Release(); err != nil {
		t.Fatalf("releasing the supplier's owner: %v", err)
	}
	publicationWaitCounts(t, p.B, 0, 0)
	publicationCounts(t, outgoing, 0, 0, "released supplier")
	publicationCounts(t, incoming, 0, 0, "remotely revoked attachment")
	c, cancel := ctx()
	defer cancel()
	_, err := alias(c, json.RawMessage(`42`))
	refused(t, err, live.ErrorReferenceReleased)
	holds(t, p.A, 0, 0, "released supplier scope")
	publicationAlive(t, p)
}

func handlerOwnerAfterLostReply(t T, p Pair) {
	t.Helper()
	publicationInstallAlive(t, p)
	owners := make(chan *live.Owner, 1)
	unblock, returned := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(unblock) }) }
	defer finish()
	if err := p.B.Peer().Handle("publication.return", func(c context.Context, _ *runtime.Peer, _ json.RawMessage) (any, error) {
		owner := p.B.Owner().Child()
		c = live.WithOwner(c, owner)
		selected, ok := live.OwnerOf(c)
		if !ok || selected != owner {
			return nil, fmt.Errorf("handler owner is inaccessible")
		}
		raw, err := selected.ExportValue(func(build *live.Owner) (json.RawMessage, error) {
			reference, err := build.Export(sink, echo)
			if err != nil {
				return nil, err
			}
			return json.Marshal(reference)
		})
		owners <- selected
		<-unblock
		defer close(returned)
		return raw, err
	}); err != nil {
		t.Fatalf("installing returned callback: %v", err)
	}
	c, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.A.Peer().Call(c, "publication.return", nil, nil) }()
	owner := publicationAwait(t, owners, "handler-owned result construction")
	if err := publicationAwait(t, done, "lost reply"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost reply returned %v", err)
	}
	finish()
	publicationAwait(t, returned, "the handler's late return")
	publicationCounts(t, owner, 1, 0, "reachable handler owner after lost reply")
	holds(t, p.B, 1, 0, "handler's retained return")
	holds(t, p.A, 0, 0, "caller received no native value")
	if err := owner.Release(); err != nil {
		t.Fatalf("releasing handler owner: %v", err)
	}
	publicationCounts(t, owner, 0, 0, "released handler owner")
	holds(t, p.B, 0, 0, "released handler result")
	publicationAlive(t, p)
}

func eventPublicationRetainsItsOwner(t T, p Pair) {
	t.Helper()
	publicationInstallAlive(t, p)
	outgoing, incoming := p.A.Owner().Child(), p.B.Owner().Child()
	defer outgoing.Release()
	defer incoming.Release()
	retained := make(chan live.Invoke, 1)
	failed := make(chan error, 1)
	if err := p.B.Peer().HandleEvent("publication.event", func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) {
		invoke, err := publicationImport(incoming, raw)
		if err != nil {
			failed <- err
			return
		}
		retained <- invoke
	}); err != nil {
		t.Fatalf("installing event receiver: %v", err)
	}
	if err := p.A.Peer().Emit(context.Background(), "publication.event", publicationPayload(t, outgoing)); err != nil {
		t.Fatalf("publishing event: %v", err)
	}
	var alias live.Invoke
	select {
	case alias = <-retained:
	case err := <-failed:
		t.Fatalf("receiving event: %v", err)
	case <-time.After(waited):
		t.Fatalf("event never arrived")
	}
	publicationCounts(t, outgoing, 1, 0, "event emitter")
	publicationCounts(t, incoming, 0, 1, "event receiver")
	if got := string(call(t, alias, "43")); got != "43" {
		t.Fatalf("event callback: %s", got)
	}
	if err := outgoing.Release(); err != nil {
		t.Fatalf("event owner release: %v", err)
	}
	publicationWaitCounts(t, p.B, 0, 0)
	holds(t, p.A, 0, 0, "event export released")
	publicationAlive(t, p)
}

func scopeClosureEndsRetainedOwners(t T, p Pair) {
	t.Helper()
	publicationInstallAlive(t, p)
	owner := p.A.Owner().Child()
	child := owner.Child()
	publicationPayload(t, child)
	publicationCounts(t, child, 1, 0, "retained child before scope close")
	if err := p.A.Close(); err != nil {
		t.Fatalf("scope close: %v", err)
	}
	publicationCounts(t, owner, 0, 0, "closed owner's subtree")
	publicationCounts(t, child, 0, 0, "closed child")
	holds(t, p.A, 0, 0, "closed scope")
	publicationAlive(t, p)
}

func provenUnpublishedIsUnwound(t T, p Pair) {
	t.Helper()
	owner := p.A.Owner().Child()
	defer owner.Release()
	for i := 0; i < 12; i++ {
		_, err := owner.ExportValue(func(build *live.Owner) (json.RawMessage, error) {
			if _, err := build.Export(sink, echo); err != nil {
				return nil, err
			}
			return json.RawMessage(`{`), nil
		})
		if err == nil {
			t.Fatalf("invalid unpublished payload succeeded")
		}
		publicationCounts(t, owner, 0, 0, "failed construction")
		holds(t, p.A, 0, 0, "unpublished export unwound")
		holds(t, p.B, 0, 0, "nothing reached the peer")
	}
}
