package routes_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	cellbinding "example.test/generated/api/go/compose-cell-binding"
	cell "example.test/generated/api/go/compose-cell-protocol"
	functions "example.test/generated/api/go/functions-protocol"
	generic "example.test/generated/api/go/holder-protocol"
	numbers "example.test/generated/api/go/numbers-protocol"
	texts "example.test/generated/api/go/texts-protocol"
	numberSource "example.test/generated/bound/numbers/go/holder-protocol"
	textSource "example.test/generated/bound/texts/go/holder-protocol"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

type effects struct{ Job, Notify int }
type observation struct{ Job, Notify any }
type specimen[T any] struct {
	value   func(int64, *effects) T
	observe func(context.Context, T) (observation, error)
}
type evidence struct {
	Revisions        []int64
	Observations     []observation
	Effects          effects
	Counts, Released []live.Counts
}

// Both generated construction routes use this one provider-agnostic model.
type memory[T any] struct {
	value       T
	revision    int64
	transitions []string
	sessions    int
}

func (m *memory[T]) Put(_ context.Context, input cell.Put[T]) (int64, error) {
	m.value = input.Value
	m.revision++
	m.transitions = append(m.transitions, "put")
	return m.revision, nil
}
func (m *memory[T]) Get(context.Context) (T, error) {
	m.transitions = append(m.transitions, "get")
	return m.value, nil
}

type noted[T any] struct{}

func (noted[T]) Noted(context.Context, cell.Put[T]) error { return nil }

type reverse[T any] struct{}

func (reverse[T]) Mirror(_ context.Context, input cell.Put[T]) (T, error) { return input.Value, nil }

type changed[T any] struct{}

func (changed[T]) Changed(context.Context, cell.Put[T]) error { return nil }

type connection struct {
	client, server *runtime.Peer
	near, far      *live.Scope
}

func pair(t *testing.T) connection {
	t.Helper()
	var p connection
	a, b := duplex.Pipe(8 << 20)
	var err error
	p.client, err = runtime.NewPeer(t.Context(), a, runtime.ClientRole, runtime.Options{Prepare: func(peer *runtime.Peer) (err error) { p.near, err = live.Over(peer, live.Options{}); return err }})
	if err != nil {
		t.Fatal(err)
	}
	p.server, err = runtime.NewPeer(t.Context(), b, runtime.ServerRole, runtime.Options{Prepare: func(peer *runtime.Peer) (err error) { p.far, err = live.Over(peer, live.Options{}); return err }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.client.Close(); p.server.Close() })
	return p
}
func record[T any](t *testing.T, sender, receiver runtime.ValueAdapter[T], input specimen[T]) evidence {
	t.Helper()
	p := pair(t)
	state := &memory[T]{}
	wire, err := cellbinding.ToWire(func(cell.Client[T]) (cell.Server[T], error) {
		state.sessions++
		return cell.Server[T]{Methods: state, Events: noted[T]{}}, nil
	}, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(p.far)}, receiver)
	if err != nil {
		t.Fatal(err)
	}
	defer wire.Close(duplex.CodeNormal, "")
	detach, err := runtime.ForwardWire(p.server.Wire(), wire)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	model, err := cellbinding.FromWire(ctx, p.client.Wire(), runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(p.near)}, sender)
	if err != nil {
		t.Fatal(err)
	}
	client, err := model(cell.Client[T]{Methods: reverse[T]{}, Events: changed[T]{}})
	if err != nil {
		t.Fatal(err)
	}
	owner := p.near.Owner().Child()
	ctx = live.WithOwner(ctx, owner)
	var result evidence
	var retained []T
	for seed := int64(1); seed <= 2; seed++ {
		revision, err := client.Methods.Put(ctx, cell.Put[T]{Value: input.value(seed, &result.Effects)})
		if err != nil {
			t.Fatal(err)
		}
		result.Revisions = append(result.Revisions, revision)
		value, err := client.Methods.Get(ctx)
		if err != nil {
			t.Fatal(err)
		}
		retained = append(retained, value)
	}
	// Neither original is invoked until both supplying RPCs have completed.
	for _, index := range []int{0, 1, 0} {
		seen, err := input.observe(ctx, retained[index])
		if err != nil {
			t.Fatal(err)
		}
		result.Observations = append(result.Observations, seen)
	}
	result.Counts = []live.Counts{p.near.Counts(), p.far.Counts()}
	for _, count := range result.Counts {
		if count.Exports == 0 || count.Imports == 0 {
			t.Fatalf("missing live directions: %+v", result.Counts)
		}
	}
	if !reflect.DeepEqual(result.Revisions, []int64{1, 2}) || !reflect.DeepEqual(state.transitions, []string{"put", "get", "put", "get"}) || state.sessions != 1 {
		t.Fatalf("state machine changed: %+v", state)
	}
	_ = owner.Release()
	_ = p.near.Owner().Release()
	_ = p.far.Owner().Release()
	for p.near.Counts() != (live.Counts{}) || p.far.Counts() != (live.Counts{}) {
		select {
		case <-ctx.Done():
			t.Fatalf("bindings remain before teardown: %+v %+v", p.near.Counts(), p.far.Counts())
		case <-time.After(time.Millisecond):
		}
	}
	result.Released = []live.Counts{p.near.Counts(), p.far.Counts()}
	return result
}

func checkEvidence(t *testing.T, got, want evidence) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("independent construction observations differ:\n%+v\n%+v", got, want)
	}
}

func batch[C any](value, empty C) []runtime.Nullable[map[string]C] {
	return []runtime.Nullable[map[string]C]{runtime.Null[map[string]C](), runtime.NonNull(map[string]C{"value": value, "empty": empty})}
}
func selected[C any](value []runtime.Nullable[map[string]C]) (C, error) {
	var zero C
	if len(value) != 2 || !value[0].Null || value[1].Null || len(value[1].Value) != 2 {
		return zero, errors.New("array, null or map changed")
	}
	return value[1].Value["value"], nil
}
func numberSpec[T any](pack func(numbers.Job, numbers.Progress) T, unpack func(T) (numbers.Job, numbers.Progress, error)) specimen[T] {
	return specimen[T]{
		value: func(seed int64, e *effects) T {
			job := numbers.Job{Run: func(_ context.Context, n int64) (int64, error) { e.Job++; return n + seed, nil }}
			progress := numbers.Progress{Label: "fixed", Notify: numbers.Job{Run: func(_ context.Context, n int64) (int64, error) { e.Notify++; return n + 10*seed, nil }}}
			return pack(job, progress)
		},
		observe: func(ctx context.Context, value T) (observation, error) {
			job, progress, err := unpack(value)
			if err != nil {
				return observation{}, err
			}
			if progress.Label != "fixed" {
				return observation{}, errors.New("label changed")
			}
			x, err := job.Run(ctx, 5)
			if err != nil {
				return observation{}, err
			}
			y, err := progress.Notify.Run(ctx, 5)
			return observation{x, y}, err
		},
	}
}
func textSpec[T any](pack func(texts.Job, texts.Progress) T, unpack func(T) (texts.Job, texts.Progress, error)) specimen[T] {
	return specimen[T]{
		value: func(seed int64, e *effects) T {
			job := texts.Job{Run: func(_ context.Context, n string) (string, error) { e.Job++; return fmt.Sprintf("%s:%d", n, seed), nil }}
			progress := texts.Progress{Label: "fixed", Notify: texts.Job{Run: func(_ context.Context, n string) (string, error) {
				e.Notify++
				return fmt.Sprintf("%s:%d", n, 10*seed), nil
			}}}
			return pack(job, progress)
		},
		observe: func(ctx context.Context, value T) (observation, error) {
			job, progress, err := unpack(value)
			if err != nil {
				return observation{}, err
			}
			if progress.Label != "fixed" {
				return observation{}, errors.New("label changed")
			}
			x, err := job.Run(ctx, "v")
			if err != nil {
				return observation{}, err
			}
			y, err := progress.Notify.Run(ctx, "v")
			return observation{x, y}, err
		},
	}
}

func TestIndependentHolderRoutes(t *testing.T) {
	type NG = generic.Batch[numbers.Job, numbers.Progress]
	type TG = generic.Batch[texts.Job, texts.Progress]
	ng := numberSpec(
		func(j numbers.Job, p numbers.Progress) NG {
			return batch(generic.Choice[numbers.Job, numbers.Progress]{Value: &generic.Value[numbers.Job, numbers.Progress]{Job: j, Progress: p}}, generic.Choice[numbers.Job, numbers.Progress]{Empty: &struct{}{}})
		},
		func(v NG) (numbers.Job, numbers.Progress, error) {
			c, err := selected(v)
			if err != nil || c.Value == nil || v[1].Value["empty"].Empty == nil {
				return numbers.Job{}, numbers.Progress{}, errors.New("union or empty changed")
			}
			return c.Value.Job, c.Value.Progress, nil
		},
	)
	ns := numberSpec(
		func(j numbers.Job, p numbers.Progress) numberSource.Batch {
			return batch(numberSource.Choice{Value: &numberSource.Value{Job: j, Progress: p}}, numberSource.Choice{Empty: &struct{}{}})
		},
		func(v numberSource.Batch) (numbers.Job, numbers.Progress, error) {
			c, err := selected(v)
			if err != nil || c.Value == nil || v[1].Value["empty"].Empty == nil {
				return numbers.Job{}, numbers.Progress{}, errors.New("union or empty changed")
			}
			return c.Value.Job, c.Value.Progress, nil
		},
	)
	tg := textSpec(
		func(j texts.Job, p texts.Progress) TG {
			return batch(generic.Choice[texts.Job, texts.Progress]{Value: &generic.Value[texts.Job, texts.Progress]{Job: j, Progress: p}}, generic.Choice[texts.Job, texts.Progress]{Empty: &struct{}{}})
		},
		func(v TG) (texts.Job, texts.Progress, error) {
			c, err := selected(v)
			if err != nil || c.Value == nil || v[1].Value["empty"].Empty == nil {
				return texts.Job{}, texts.Progress{}, errors.New("union or empty changed")
			}
			return c.Value.Job, c.Value.Progress, nil
		},
	)
	ts := textSpec(
		func(j texts.Job, p texts.Progress) textSource.Batch {
			return batch(textSource.Choice{Value: &textSource.Value{Job: j, Progress: p}}, textSource.Choice{Empty: &struct{}{}})
		},
		func(v textSource.Batch) (texts.Job, texts.Progress, error) {
			c, err := selected(v)
			if err != nil || c.Value == nil || v[1].Value["empty"].Empty == nil {
				return texts.Job{}, texts.Progress{}, errors.New("union or empty changed")
			}
			return c.Value.Job, c.Value.Progress, nil
		},
	)
	numbersAdapter := generic.AdapterBatch(numbers.AdapterJob(), numbers.AdapterProgress())
	textsAdapter := generic.AdapterBatch(texts.AdapterJob(), texts.AdapterProgress())
	nr := record(t, numbersAdapter, numbersAdapter, ng)
	tr := record(t, textsAdapter, textsAdapter, tg)
	checkEvidence(t, record(t, numberSource.AdapterBatch(), numberSource.AdapterBatch(), ns), nr)
	checkEvidence(t, record(t, textSource.AdapterBatch(), textSource.AdapterBatch(), ts), tr)
	if !reflect.DeepEqual(nr.Observations, []observation{{int64(6), int64(15)}, {int64(7), int64(25)}, {int64(6), int64(15)}}) || !reflect.DeepEqual(tr.Observations, []observation{{"v:1", "v:10"}, {"v:2", "v:20"}, {"v:1", "v:10"}}) {
		t.Fatalf("wrong provider observations: %+v %+v", nr, tr)
	}
	for _, r := range []evidence{nr, tr} {
		if r.Effects != (effects{3, 3}) {
			t.Fatalf("wrong callback multiplicity: %+v", r)
		}
	}
	mismatch(t, numbersAdapter, textsAdapter)
	mismatch(t, textsAdapter, numbersAdapter)
}

func TestIndependentCallableRoutes(t *testing.T) {
	integer := runtime.JSONAdapter[int64]()
	genericAdapter := functions.AdapterFunction(integer, integer)
	sourceAdapter := functions.AdapterIntegerFunction()
	identity, err := functions.ContractFunction(integer, integer)
	if err != nil {
		t.Fatal(err)
	}
	specialized, err := functions.ContractIntegerFunction()
	if err != nil {
		t.Fatal(err)
	}
	if identity != specialized || identity.Path != "functions/Function<integer,integer>" {
		t.Fatalf("nominal provenance lost: %+v %+v", identity, specialized)
	}
	input := specimen[functions.IntegerFunction]{
		value: func(seed int64, e *effects) functions.IntegerFunction {
			return func(_ context.Context, n int64) (int64, error) { e.Job++; return n + seed, nil }
		},
		observe: func(ctx context.Context, fn functions.IntegerFunction) (observation, error) {
			value, err := fn(ctx, 5)
			return observation{value, int64(0)}, err
		},
	}
	expected := record(t, genericAdapter, genericAdapter, input)
	for _, adapters := range [][2]runtime.ValueAdapter[functions.IntegerFunction]{{sourceAdapter, sourceAdapter}, {genericAdapter, sourceAdapter}, {sourceAdapter, genericAdapter}} {
		checkEvidence(t, record(t, adapters[0], adapters[1], input), expected)
	}
	if !reflect.DeepEqual(expected.Observations, []observation{{int64(6), int64(0)}, {int64(7), int64(0)}, {int64(6), int64(0)}}) || expected.Effects != (effects{3, 0}) {
		t.Fatalf("wrong callable observations: %+v", expected)
	}
	other := functions.AdapterOtherFunction(integer, integer)
	mismatch(t, genericAdapter, other)
	mismatch(t, other, sourceAdapter)
}

func mismatch[A, B any](t *testing.T, serverAdapter runtime.ValueAdapter[A], clientAdapter runtime.ValueAdapter[B]) {
	t.Helper()
	p := pair(t)
	state := &memory[A]{}
	wire, err := cellbinding.ToWire(func(cell.Client[A]) (cell.Server[A], error) {
		state.sessions++
		return cell.Server[A]{Methods: state, Events: noted[A]{}}, nil
	}, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(p.far)}, serverAdapter)
	if err != nil {
		t.Fatal(err)
	}
	defer wire.Close(duplex.CodeNormal, "")
	detach, err := runtime.ForwardWire(p.server.Wire(), wire)
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err = cellbinding.FromWire(ctx, p.client.Wire(), runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(p.near)}, clientAdapter)
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != "contract_mismatch" {
		t.Fatalf("wrong refusal: %v", err)
	}
	if state.sessions != 1 || len(state.transitions) != 0 || p.near.Counts() != (live.Counts{}) || p.far.Counts() != (live.Counts{}) {
		t.Fatal("refusal performed model effects or acquired live bindings")
	}
}
