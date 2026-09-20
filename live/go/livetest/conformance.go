// Package livetest is the live layer's shared suite: the behavior a scope
// promises, written once and run by both languages against their own runtime.
// It is the twin of duplextest for the seam and sessiontest for the session,
// and its TypeScript counterpart is live/ts/src/conformance.ts — the same
// cases, in the same order, under the same names.
//
// Every case ends by counting what each scope still holds. A binding nobody
// released is a leak, and a suite that only compares payloads never sees one.
package livetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// T is what a case reports through — testing.T, or whatever a language's suite
// gives it.
type T interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Pair is two scopes over one connection, as a language makes them: a and b
// speak to each other and nothing else. Close ends both.
type Pair struct {
	A     *live.Scope
	B     *live.Scope
	Close func()
}

// Make is how a language builds a pair for the suite.
type Make func(t T) Pair

// Run holds a runtime to every case of the suite.
func Run(t T, make Make) {
	t.Helper()
	for _, c := range Cases() {
		c := c
		func() {
			p := make(t)
			defer p.Close()
			c.Run(t, p)
		}()
	}
}

// Case is one named behavior of the suite.
type Case struct {
	Name string
	Run  func(t T, p Pair)
}

// Cases is the suite, in order.
func Cases() []Case {
	return []Case{
		{"a callback reaches the side that supplied it", callbackAndResult},
		{"a returned callable outlives the call that returned it", higherOrder},
		{"two suppliers are told apart", independentSuppliers},
		{"one binding imported twice is one attachment", aliases},
		{"a reference of another scope is refused", foreignReference},
		{"a contract the binding does not carry is refused", contractMismatch},
		{"a binding nobody exported is refused", unknownReference},
		{"release refuses the next invocation and settles the one in flight", releaseIsABarrier},
		{"release invalidates every alias", releaseInvalidatesAliases},
		{"cancelling an invocation is not releasing the binding", cancellationIsNotRelease},
		{"a pre-cancelled invocation does not dispatch", preCancelledInvocationDoesNotDispatch},
		{"a scope that closes settles what it had in flight", closeSettles},
		{"closing the exporter settles a call without closing the peer", closeExporterSettles},
		{"closing the scope settles a local self-reference call", closeLocalSettles},
		{"a reference handed back to its exporter needs no wire", selfReference},
		{"forwarding gives the destination its own lifetime", forwarding},
		{"a refused export leaves no binding behind", boundsLeaveNothing},
	}
}

const (
	sink   = "probe/Report"
	job    = "probe/Cancel"
	other  = "probe/SetVolume"
	waited = 5 * time.Second
)

// echo is the callable every case uses where the body does not matter: it
// answers with what it was asked.
func echo(_ context.Context, request json.RawMessage) (json.RawMessage, error) {
	if request == nil {
		return json.RawMessage("null"), nil
	}
	return request, nil
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), waited)
}

func call(t T, invoke live.Invoke, request string) json.RawMessage {
	t.Helper()
	c, cancel := ctx()
	defer cancel()
	result, err := invoke(c, json.RawMessage(request))
	if err != nil {
		t.Fatalf("invoking: %v", err)
	}
	return result
}

func refused(t T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s, got no refusal at all", code)
		return
	}
	var public *runtime.PublicError
	if !errors.As(err, &public) {
		t.Fatalf("expected the public code %s, got %v", code, err)
		return
	}
	if public.Code != code {
		t.Errorf("expected %s, got %s: %s", code, public.Code, public.Message)
	}
}

func holds(t T, s *live.Scope, exports, imports int, where string) {
	t.Helper()
	got := s.Counts()
	if got.Exports != exports || got.Imports != imports {
		t.Errorf("%s: the scope holds %d exports and %d imports, expected %d and %d",
			where, got.Exports, got.Imports, exports, imports)
	}
}

// handed exports a callable on one side and imports it on the other, the way a
// payload carrying a reference would: the reference is marshalled, crosses, and
// is decoded by the scope that received it.
func handed(t T, from, to *live.Scope, contract string, invoke live.Invoke) (live.Reference, live.Invoke) {
	t.Helper()
	exported, err := from.Export(contract, invoke)
	if err != nil {
		t.Fatalf("exporting: %v", err)
	}
	raw, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("marshalling a reference: %v", err)
	}
	arrived, err := to.Decode(raw)
	if err != nil {
		t.Fatalf("decoding a reference: %v", err)
	}
	imported, err := to.Import(arrived, contract)
	if err != nil {
		t.Fatalf("importing: %v", err)
	}
	return arrived, imported
}

func callbackAndResult(t T, p Pair) {
	t.Helper()
	reported := make(chan string, 4)
	_, invoke := handed(t, p.A, p.B, sink, func(_ context.Context, request json.RawMessage) (json.RawMessage, error) {
		reported <- string(request)
		return json.RawMessage("null"), nil
	})
	call(t, invoke, "50")
	select {
	case got := <-reported:
		if got != "50" {
			t.Errorf("the callback was asked %s, expected 50", got)
		}
	case <-time.After(waited):
		t.Fatalf("the callback was never reached")
	}
	holds(t, p.A, 1, 0, "the supplier")
	holds(t, p.B, 0, 1, "the side it was supplied to")
}

// higherOrder is the case the whole layer exists for: a callable supplied to a
// call is invoked after that call has returned.
func higherOrder(t T, p Pair) {
	t.Helper()
	reported := make(chan string, 4)
	_, progress := handed(t, p.A, p.B, sink, func(_ context.Context, request json.RawMessage) (json.RawMessage, error) {
		reported <- string(request)
		return json.RawMessage("null"), nil
	})
	// The exchange that introduced the reference is over; the reference is not.
	_, cancelJob := handed(t, p.B, p.A, job, func(c context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return progress(c, json.RawMessage("100"))
	})
	call(t, cancelJob, "null")
	select {
	case got := <-reported:
		if got != "100" {
			t.Errorf("the retained callback was asked %s, expected 100", got)
		}
	case <-time.After(waited):
		t.Fatalf("a callable returned by a call could not reach the callback that call supplied")
	}
}

func independentSuppliers(t T, p Pair) {
	t.Helper()
	first := make(chan string, 2)
	second := make(chan string, 2)
	_, one := handed(t, p.A, p.B, sink, func(_ context.Context, r json.RawMessage) (json.RawMessage, error) {
		first <- string(r)
		return nil, nil
	})
	_, two := handed(t, p.A, p.B, sink, func(_ context.Context, r json.RawMessage) (json.RawMessage, error) {
		second <- string(r)
		return nil, nil
	})
	call(t, one, "1")
	call(t, two, "2")
	if got := <-first; got != "1" {
		t.Errorf("the first supplier was asked %s", got)
	}
	if got := <-second; got != "2" {
		t.Errorf("the second supplier was asked %s", got)
	}
	if len(first) != 0 || len(second) != 0 {
		t.Errorf("a supplier was asked what the other was asked")
	}
	holds(t, p.B, 0, 2, "two suppliers are two bindings")
}

func aliases(t T, p Pair) {
	t.Helper()
	arrived, once := handed(t, p.A, p.B, sink, echo)
	again, err := p.B.Import(arrived, sink)
	if err != nil {
		t.Fatalf("importing a binding twice: %v", err)
	}
	holds(t, p.B, 0, 1, "one binding imported twice")
	// Both aliases work, and neither takes the other's reply: two attachments
	// on one binding would be two readers competing for one answer.
	if got := string(call(t, once, "1")); got != "1" {
		t.Errorf("the first alias got %s", got)
	}
	if got := string(call(t, again, "2")); got != "2" {
		t.Errorf("the second alias got %s", got)
	}
}

func foreignReference(t T, p Pair) {
	t.Helper()
	exported, err := p.A.Export(sink, echo)
	if err != nil {
		t.Fatalf("exporting: %v", err)
	}
	// The reference was minted in A's scope; B never decoded it.
	_, err = p.B.Import(exported, sink)
	refused(t, err, live.ErrorReferenceForeign)
	holds(t, p.B, 0, 0, "a foreign reference attaches nothing")
}

func contractMismatch(t T, p Pair) {
	t.Helper()
	exported, err := p.A.Export(sink, echo)
	if err != nil {
		t.Fatalf("exporting: %v", err)
	}
	raw, _ := json.Marshal(exported)
	arrived, err := p.B.Decode(raw)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	_, err = p.B.Import(arrived, other)
	refused(t, err, live.ErrorContractMismatch)
	holds(t, p.B, 0, 0, "a mismatched contract attaches nothing")
}

// unknownReference is the stale token: a binding id of no scope, which is what
// a reference carried out of its connection and back in becomes.
func unknownReference(t T, p Pair) {
	t.Helper()
	arrived, err := p.B.Decode(json.RawMessage(`{"binding":"0000000000000000.1","contract":"` + sink + `"}`))
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	invoke, err := p.B.Import(arrived, sink)
	if err != nil {
		t.Fatalf("importing: %v", err)
	}
	c, cancel := ctx()
	defer cancel()
	_, err = invoke(c, nil)
	refused(t, err, live.ErrorReferenceUnknown)
}

// releaseIsABarrier is the operator's verdict: the next invocation is refused,
// and the one already dispatched settles and is delivered.
func releaseIsABarrier(t T, p Pair) {
	t.Helper()
	entered := make(chan struct{})
	let := make(chan struct{})
	arrived, invoke := handed(t, p.A, p.B, sink, func(_ context.Context, r json.RawMessage) (json.RawMessage, error) {
		close(entered)
		<-let
		return json.RawMessage(`"settled"`), nil
	})

	inflight := make(chan error, 1)
	settled := make(chan json.RawMessage, 1)
	go func() {
		c, cancel := ctx()
		defer cancel()
		result, err := invoke(c, nil)
		inflight <- err
		settled <- result
	}()
	select {
	case <-entered:
	case <-time.After(waited):
		t.Fatalf("the invocation never reached the binding")
	}

	if err := p.B.Release(arrived); err != nil {
		t.Fatalf("releasing: %v", err)
	}

	// New invocations are refused at once, while the first is still running.
	c, cancel := ctx()
	defer cancel()
	_, err := invoke(c, nil)
	refused(t, err, live.ErrorReferenceReleased)

	close(let)
	if err := <-inflight; err != nil {
		t.Errorf("the invocation in flight was not allowed to settle: %v", err)
	}
	if got := string(<-settled); got != `"settled"` {
		t.Errorf("the invocation in flight settled with %s", got)
	}
	holds(t, p.B, 0, 0, "the released import")
}

func releaseInvalidatesAliases(t T, p Pair) {
	t.Helper()
	arrived, once := handed(t, p.A, p.B, sink, echo)
	again, err := p.B.Import(arrived, sink)
	if err != nil {
		t.Fatalf("importing twice: %v", err)
	}
	if err := p.B.Release(arrived); err != nil {
		t.Fatalf("releasing: %v", err)
	}
	c, cancel := ctx()
	defer cancel()
	_, err = once(c, nil)
	refused(t, err, live.ErrorReferenceReleased)
	_, err = again(c, nil)
	refused(t, err, live.ErrorReferenceReleased)
	// And importing it again is refused for the reason it was refused for.
	_, err = p.B.Import(arrived, sink)
	refused(t, err, live.ErrorReferenceReleased)
}

// cancellationIsNotRelease holds the three meanings apart: withdrawing an
// invocation withdraws that invocation and nothing else.
func cancellationIsNotRelease(t T, p Pair) {
	t.Helper()
	entered := make(chan struct{}, 2)
	let := make(chan struct{})
	var once sync.Once
	_, invoke := handed(t, p.A, p.B, sink, func(c context.Context, r json.RawMessage) (json.RawMessage, error) {
		if string(r) == `"wait"` {
			once.Do(func() { entered <- struct{}{} })
			select {
			case <-let:
			case <-c.Done():
				return nil, c.Err()
			}
			return json.RawMessage(`"late"`), nil
		}
		return r, nil
	})

	withdrawn, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := invoke(withdrawn, json.RawMessage(`"wait"`))
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(waited):
		t.Fatalf("the invocation never reached the binding")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Errorf("a withdrawn invocation answered")
		}
	case <-time.After(waited):
		t.Fatalf("a withdrawn invocation never settled")
	}
	close(let)

	// The binding is untouched: cancelling an invocation released nothing.
	if got := string(call(t, invoke, `"again"`)); got != `"again"` {
		t.Errorf("the binding did not survive a cancelled invocation: %s", got)
	}
	holds(t, p.B, 0, 1, "a cancelled invocation releases no binding")
}

func preCancelledInvocationDoesNotDispatch(t T, p Pair) {
	t.Helper()
	var dispatched atomic.Int32
	_, invoke := handed(t, p.A, p.B, sink, func(_ context.Context, r json.RawMessage) (json.RawMessage, error) {
		dispatched.Add(1)
		return r, nil
	})
	withdrawn, cancel := context.WithCancel(context.Background())
	cancel()

	// More cancelled attempts than the default pending-request bound must
	// leave room for the subsequent call on this same open connection.
	for attempt := 0; attempt < 256; attempt++ {
		if _, err := invoke(withdrawn, json.RawMessage(`"cancelled"`)); !errors.Is(err, context.Canceled) {
			t.Fatalf("an already-cancelled invocation returned %v", err)
		}
	}
	if got := dispatched.Load(); got != 0 {
		t.Errorf("%d already-cancelled invocations reached the binding", got)
	}
	holds(t, p.A, 1, 0, "pre-cancellation leaves the exported binding intact")
	holds(t, p.B, 0, 1, "pre-cancellation leaves the imported binding intact")

	if got := string(call(t, invoke, `"again"`)); got != `"again"` {
		t.Errorf("the binding did not survive a pre-cancelled invocation: %s", got)
	}
	if got := dispatched.Load(); got != 1 {
		t.Errorf("the fresh invocation should be the only dispatch, got %d", got)
	}
	holds(t, p.A, 1, 0, "the fresh invocation retains one export")
	holds(t, p.B, 0, 1, "the fresh invocation retains one import")
}

func closeSettles(t T, p Pair) {
	t.Helper()
	entered := make(chan struct{})
	let := make(chan struct{})
	defer close(let)
	_, invoke := handed(t, p.A, p.B, sink, func(c context.Context, _ json.RawMessage) (json.RawMessage, error) {
		close(entered)
		<-let
		return nil, nil
	})
	done := make(chan error, 1)
	go func() {
		c, cancel := ctx()
		defer cancel()
		_, err := invoke(c, nil)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(waited):
		t.Fatalf("the invocation never reached the binding")
	}
	if err := p.B.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Errorf("a scope that closed left an invocation answered")
		}
	case <-time.After(waited):
		t.Fatalf("a scope that closed never settled what it had in flight")
	}
	holds(t, p.B, 0, 0, "a closed scope holds nothing")
}

// selfReference is a reference of this side's own making, handed back. It must
// reach the function behind it rather than attach a second dispatch to a
// binding this side already serves.
func selfReference(t T, p Pair) {
	t.Helper()
	asked := make(chan struct{}, 2)
	exported, err := p.A.Export(sink, func(_ context.Context, r json.RawMessage) (json.RawMessage, error) {
		asked <- struct{}{}
		return r, nil
	})
	if err != nil {
		t.Fatalf("exporting: %v", err)
	}
	raw, _ := json.Marshal(exported)
	back, err := p.A.Decode(raw)
	if err != nil {
		t.Fatalf("decoding our own reference: %v", err)
	}
	invoke, err := p.A.Import(back, sink)
	if err != nil {
		t.Fatalf("importing our own reference: %v", err)
	}
	if got := string(call(t, invoke, "7")); got != "7" {
		t.Errorf("our own binding answered %s", got)
	}
	<-asked
	holds(t, p.A, 1, 0, "our own reference is no import")
}

func forwarding(t T, p Pair) {
	t.Helper()
	// A exports, B imports, and B forwards it back to A as a binding of its
	// own. Releasing the forwarded binding must not release the origin.
	origin, imported := handed(t, p.A, p.B, sink, echo)
	forwarded, err := live.Forward(p.B, sink, imported)
	if err != nil {
		t.Fatalf("forwarding: %v", err)
	}
	raw, _ := json.Marshal(forwarded)
	arrived, err := p.A.Decode(raw)
	if err != nil {
		t.Fatalf("decoding the forwarded reference: %v", err)
	}
	through, err := p.A.Import(arrived, sink)
	if err != nil {
		t.Fatalf("importing the forwarded reference: %v", err)
	}
	if got := string(call(t, through, "3")); got != "3" {
		t.Errorf("the forwarded binding answered %s", got)
	}

	if err := p.B.Release(forwarded); err != nil {
		t.Fatalf("releasing the forwarded binding: %v", err)
	}
	// The origin is untouched, which is what taking no ownership means.
	if got := string(call(t, imported, "4")); got != "4" {
		t.Errorf("releasing the forwarded binding disturbed its origin: %s", got)
	}
	_ = origin
}

// boundsLeaveNothing is the leak assertion the suite exists for: a refused
// export registers no binding and the scope holds exactly what it held before.
func boundsLeaveNothing(t T, p Pair) {
	t.Helper()
	before := p.A.Counts()
	_, err := p.A.Export("", echo)
	refused(t, err, live.ErrorContractInvalid)
	_, err = p.A.Export(sink, nil)
	refused(t, err, live.ErrorContractInvalid)
	if got := p.A.Counts(); got != before {
		t.Errorf("a refused export left %v behind, expected %v", got, before)
	}
}

// Describe names the suite for a report.
func Describe() string { return fmt.Sprintf("the live layer, %d cases", len(Cases())) }
