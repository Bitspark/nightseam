package composition_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	functions "example.test/generated/api/go/functions-protocol"
	holder "example.test/generated/api/go/holder-protocol"
	numbers "example.test/generated/api/go/numbers-protocol"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

type unary = functions.Function[int64, int64]
type factory = functions.Function[unary, unary]
type held = holder.Value[numbers.Job, numbers.Progress]
type batch = holder.Batch[numbers.Job, numbers.Progress]

func pair(t *testing.T, maximum int) (*live.Scope, *live.Scope) {
	t.Helper()
	a, b := duplex.Pipe(1 << 20)
	var sa, sb *live.Scope
	prepare := func(scope **live.Scope) runtime.Options {
		return runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
			*scope, err = live.Over(p, live.Options{MaxImports: maximum})
			return err
		}}
	}
	pa, err := runtime.NewPeer(t.Context(), a, runtime.ClientRole, prepare(&sa))
	if err != nil {
		t.Fatal(err)
	}
	pb, err := runtime.NewPeer(t.Context(), b, runtime.ServerRole, prepare(&sb))
	if err != nil {
		_ = pa.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pa.Close(); _ = pb.Close() })
	return sa, sb
}

func exportValue[T any](owner *live.Owner, adapter runtime.ValueAdapter[T], value T) (json.RawMessage, error) {
	return live.ValueEnvironment(owner.Scope()).Export(live.WithOwner(context.Background(), owner), func(ctx context.Context) (json.RawMessage, error) {
		return adapter.Export(ctx, value)
	})
}

func importValue[T any](owner *live.Owner, adapter runtime.ValueAdapter[T], raw json.RawMessage) (T, error) {
	var value T
	err := live.ValueEnvironment(owner.Scope()).Import(live.WithOwner(context.Background(), owner), func(ctx context.Context) error {
		var err error
		value, err = adapter.Import(ctx, raw)
		return err
	})
	return value, err
}

func transfer[T any](t *testing.T, source, destination *live.Owner, adapter runtime.ValueAdapter[T], value T) T {
	t.Helper()
	raw, err := exportValue(source, adapter, value)
	if err != nil {
		t.Fatal(err)
	}
	got, err := importValue(destination, adapter, raw)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func counts(t *testing.T, scope *live.Scope, want live.Counts) {
	t.Helper()
	if got := scope.Counts(); got != want {
		t.Fatalf("scope before teardown: %+v, want %+v", got, want)
	}
}

func zero(t *testing.T, scopes ...*live.Scope) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, scope := range scopes {
		for scope.Counts() != (live.Counts{}) {
			if time.Now().After(deadline) {
				t.Fatalf("bindings survived explicit release: %+v", scope.Counts())
			}
			time.Sleep(time.Millisecond)
		}
	}
}

func code(t *testing.T, err error, want string) {
	t.Helper()
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

func plain(offset int64) unary {
	return func(_ context.Context, n int64) (int64, error) { return n + offset, nil }
}

func specimen(offset int64) held {
	return held{Job: numbers.Job{Run: plain(offset)}, Progress: numbers.Progress{Label: "fixed", Notify: numbers.Job{Run: plain(offset + 10)}}}
}

func packed(value held) batch {
	return batch{
		{Null: true},
		{Value: map[string]holder.Choice[numbers.Job, numbers.Progress]{
			"entry": {Value: &holder.ChoiceValueValue[numbers.Job, numbers.Progress]{Value: value}},
			"empty": {Empty: &struct{}{}},
		}},
	}
}

func unpack(value batch) held { return value[1].Value["entry"].Value.Value }

func TestNestedDrawRollbackPreservesBorrowedAndUnrelatedOwners(t *testing.T) {
	sa, sb := pair(t, 4)
	sender, receiver := sa.Owner().Child(), sb.Owner().Child()
	unrelatedA, unrelatedB := sa.Owner().Child(), sb.Owner().Child()
	failed, freshOwner := sb.Owner().Child(), sa.Owner().Child()
	number := runtime.JSONAdapter[int64]()
	unaryAdapter := functions.AdapterFunction(number, number)
	// The diagnostic wrapper reads the active owner passed by the generated
	// walk. It neither chooses a lifetime nor captures a particular scope.
	progress := numbers.AdapterProgress()
	encode, decode := progress.Export, progress.Import
	peakExports, peakImports := 0, 0
	progress.Export = func(ctx context.Context, value numbers.Progress) (json.RawMessage, error) {
		owner, _ := live.OwnerOf(ctx)
		peakExports = max(peakExports, owner.Scope().Counts().Exports)
		return encode(ctx, value)
	}
	progress.Import = func(ctx context.Context, raw json.RawMessage) (numbers.Progress, error) {
		owner, _ := live.OwnerOf(ctx)
		peakImports = max(peakImports, owner.Scope().Counts().Imports)
		return decode(ctx, raw)
	}
	recipe := holder.AdapterBatch(numbers.AdapterJob(), progress)
	raw, err := exportValue(sender, recipe, packed(specimen(10)))
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := importValue(receiver, recipe, raw)
	if err != nil {
		t.Fatal(err)
	}
	otherB := transfer(t, unrelatedA, unrelatedB, unaryAdapter, plain(100))
	otherA := transfer(t, unrelatedB, unrelatedA, unaryAdapter, plain(200))
	counts(t, sa, live.Counts{Exports: 3, Imports: 1})
	counts(t, sb, live.Counts{Exports: 1, Imports: 3})
	fresh, err := exportValue(freshOwner, recipe, packed(specimen(30)))
	if err != nil {
		t.Fatal(err)
	}
	var oldRows, newRows []json.RawMessage
	if err := json.Unmarshal(raw, &oldRows); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fresh, &newRows); err != nil {
		t.Fatal(err)
	}
	joined, err := json.Marshal([]json.RawMessage{oldRows[0], oldRows[1], newRows[1]})
	if err != nil {
		t.Fatal(err)
	}
	peakImports = 0
	_, err = importValue(failed, recipe, joined)
	code(t, err, live.ErrorTooManyImports)
	if peakImports != 4 {
		t.Fatalf("no fresh nested attachment preceded failure: peak=%d", peakImports)
	}
	if failed.Counts() != (live.Counts{}) || receiver.Counts() != (live.Counts{Imports: 2}) || unrelatedB.Counts() != (live.Counts{Exports: 1, Imports: 1}) {
		t.Fatalf("import rollback changed owner ledgers: failed=%+v borrowed=%+v unrelated=%+v", failed.Counts(), receiver.Counts(), unrelatedB.Counts())
	}
	counts(t, sb, live.Counts{Exports: 1, Imports: 3})
	// The first row reuses both borrowed aliases. The following row creates a
	// fresh Job export before Progress's nested nil callable refuses.
	bad := packed(specimen(50))
	bad[1].Value["entry"].Value.Value.Progress.Notify.Run = nil
	bad = append(packed(unpack(borrowed)), bad[1])
	peakExports = 0
	_, err = exportValue(failed, recipe, bad)
	if err == nil {
		t.Fatal("invalid nested draw exported")
	}
	if peakExports <= 1 {
		t.Fatal("export failure happened before any fresh export")
	}
	if failed.Counts() != (live.Counts{}) {
		t.Fatalf("failed export retained %+v", failed.Counts())
	}
	counts(t, sb, live.Counts{Exports: 1, Imports: 3})
	ctx := live.WithOwner(t.Context(), receiver)
	if n, err := unpack(borrowed).Job.Run(ctx, 1); err != nil || n != 11 {
		t.Fatalf("borrowed job: %d %v", n, err)
	}
	if n, err := unpack(borrowed).Progress.Notify.Run(ctx, 1); err != nil || n != 21 {
		t.Fatalf("borrowed progress: %d %v", n, err)
	}
	if n, err := otherB(ctx, 1); err != nil || n != 101 {
		t.Fatalf("unrelated import: %d %v", n, err)
	}
	if n, err := otherA(t.Context(), 1); err != nil || n != 201 {
		t.Fatalf("unrelated export: %d %v", n, err)
	}
	_ = failed.Release()
	_ = receiver.Release()
	if n, err := otherB(t.Context(), 2); err != nil || n != 102 {
		t.Fatalf("releasing one child invalidated sibling: %d %v", n, err)
	}
	if n, err := otherA(t.Context(), 2); err != nil || n != 202 {
		t.Fatalf("releasing one child invalidated unrelated export: %d %v", n, err)
	}
	for _, owner := range []*live.Owner{sender, unrelatedA, unrelatedB, freshOwner} {
		_ = owner.Release()
	}
	zero(t, sa, sb)
}

// These policies belong to concrete exposures. Every protected invocation
// rereads the mutable policy; the reusable generated recipe holds neither it
// nor a principal. This is the synthetic #355 construction, not #356's API.
type policy struct {
	allowed                    atomic.Bool
	factories, calls, supplied atomic.Int64
	offset                     int64
}

func newPolicy(offset int64) *policy { p := &policy{offset: offset}; p.allowed.Store(true); return p }
func (p *policy) check() error {
	if !p.allowed.Load() {
		return &runtime.PublicError{Code: "denied", Message: "consumer exposure revoked"}
	}
	return nil
}
func (p *policy) snapshot() [3]int64 {
	return [3]int64{p.factories.Load(), p.calls.Load(), p.supplied.Load()}
}
func (p *policy) callback() unary {
	return func(_ context.Context, n int64) (int64, error) {
		if err := p.check(); err != nil {
			return 0, err
		}
		p.supplied.Add(1)
		return n + 3, nil
	}
}
func (p *policy) function() factory {
	return func(_ context.Context, callback unary) (unary, error) {
		if err := p.check(); err != nil {
			return nil, err
		}
		p.factories.Add(1)
		return func(ctx context.Context, n int64) (int64, error) {
			if err := p.check(); err != nil {
				return 0, err
			}
			p.calls.Add(1)
			got, err := callback(ctx, n)
			return got + p.offset, err
		}, nil
	}
}
func (p *policy) draw() held {
	call := func(_ context.Context, n int64) (int64, error) {
		if err := p.check(); err != nil {
			return 0, err
		}
		p.calls.Add(1)
		return n + p.offset, nil
	}
	return held{Job: numbers.Job{Run: call}, Progress: numbers.Progress{Label: "fixed", Notify: numbers.Job{Run: call}}}
}

type exposure[T any] struct {
	scopes                            []*live.Scope
	owners                            []*live.Owner
	p                                 *policy
	self, remote, returned, forwarded T
}

func expose[T any](t *testing.T, recipe runtime.ValueAdapter[T], value T, p *policy) exposure[T] {
	// A -> B and B -> C are independent physical peers and live scopes.
	a, b := pair(t, 128)
	c, d := pair(t, 128)
	e := exposure[T]{scopes: []*live.Scope{a, b, c, d}, p: p}
	for _, scope := range e.scopes {
		e.owners = append(e.owners, scope.Owner().Child())
	}
	raw, err := exportValue(e.owners[0], recipe, value)
	if err != nil {
		t.Fatal(err)
	}
	e.self, err = importValue(e.owners[0], recipe, raw)
	if err != nil {
		t.Fatal(err)
	}
	e.remote, err = importValue(e.owners[1], recipe, raw)
	if err != nil {
		t.Fatal(err)
	}
	e.returned = transfer(t, e.owners[1], e.owners[0], recipe, e.remote)
	e.forwarded = transfer(t, e.owners[2], e.owners[3], recipe, e.remote)
	return e
}

func (e exposure[T]) release(t *testing.T) {
	for _, owner := range e.owners {
		_ = owner.Release()
	}
	zero(t, e.scopes...)
}

func exerciseExposures[T any](t *testing.T, recipe runtime.ValueAdapter[T], makeValue func(*policy) T, capture func(context.Context, T, *policy) (func() error, error)) {
	t.Helper()
	var exposures []exposure[T]
	for _, offset := range []int64{10, 20} {
		p := newPolicy(offset)
		exposures = append(exposures, expose(t, recipe, makeValue(p), p))
	}
	retained := make([][]func() error, 2)
	for i, e := range exposures {
		for j, value := range []T{e.self, e.remote, e.returned, e.forwarded} {
			owner := e.owners[0]
			if j == 1 {
				owner = e.owners[1]
			}
			if j == 3 {
				owner = e.owners[3]
			}
			invoke, err := capture(live.WithOwner(t.Context(), owner), value, e.p)
			if err != nil {
				t.Fatal(err)
			}
			retained[i] = append(retained[i], invoke)
		}
	}
	// Both sets remain live concurrently and use the exact same recipe object.
	var wg sync.WaitGroup
	errorsCh := make(chan error, 8)
	for _, group := range retained {
		for _, invoke := range group {
			wg.Add(1)
			go func() { defer wg.Done(); errorsCh <- invoke() }()
		}
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range exposures {
		for _, scope := range e.scopes {
			if scope.Owner().Counts() != (live.Counts{}) {
				t.Fatal("recipe captured root owner")
			}
		}
	}
	exposures[0].p.allowed.Store(false)
	before := exposures[0].p.snapshot()
	for _, invoke := range retained[0] {
		code(t, invoke(), "denied")
	}
	if exposures[0].p.snapshot() != before {
		t.Fatal("revocation allowed a protected effect")
	}
	secondCounts := make([]live.Counts, 4)
	for i, scope := range exposures[1].scopes {
		secondCounts[i] = scope.Counts()
	}
	exposures[0].release(t)
	for i, scope := range exposures[1].scopes {
		counts(t, scope, secondCounts[i])
	}
	for _, invoke := range retained[1] {
		if err := invoke(); err != nil {
			t.Fatalf("other principal/lifetime lost: %v", err)
		}
	}
	exposures[1].p.allowed.Store(false)
	before = exposures[1].p.snapshot()
	for _, invoke := range retained[1] {
		code(t, invoke(), "denied")
	}
	if exposures[1].p.snapshot() != before {
		t.Fatal("second principal bypassed revocation")
	}
	exposures[1].release(t)
}

func TestReusableGenericAndSourceAliasGuards(t *testing.T) {
	number := runtime.JSONAdapter[int64]()
	unaryRecipe := functions.AdapterFunction(number, number)
	for _, route := range []struct {
		name   string
		recipe runtime.ValueAdapter[factory]
	}{
		{"derive-then-supply", functions.AdapterFunction(unaryRecipe, unaryRecipe)},
		{"source-specialized-alias", functions.AdapterFactory()},
	} {
		t.Run(route.name, func(t *testing.T) {
			exerciseExposures(t, route.recipe, func(p *policy) factory { return p.function() }, func(ctx context.Context, value factory, p *policy) (func() error, error) {
				returned, err := value(ctx, p.callback())
				if err != nil {
					return nil, err
				}
				// Invoke only after the supplying generic callable has returned.
				return func() error {
					got, err := returned(ctx, 5)
					if err != nil {
						return err
					}
					if want := p.offset + 8; got != want {
						return fmt.Errorf("wrong exposure: %d, want %d", got, want)
					}
					return nil
				}, nil
			})
		})
	}
}

func TestReusableFamilyDrawGuards(t *testing.T) {
	recipe := holder.AdapterValue(numbers.AdapterJob(), numbers.AdapterProgress())
	exerciseExposures(t, recipe, func(p *policy) held { return p.draw() }, func(ctx context.Context, value held, p *policy) (func() error, error) {
		return func() error {
			if value.Progress.Label != "fixed" {
				return errors.New("draw label changed")
			}
			for _, call := range []unary{value.Job.Run, value.Progress.Notify.Run} {
				got, err := call(ctx, 5)
				if err != nil {
					return err
				}
				if want := p.offset + 5; got != want {
					return fmt.Errorf("wrong draw exposure: %d, want %d", got, want)
				}
			}
			return nil
		}, nil
	})
}
