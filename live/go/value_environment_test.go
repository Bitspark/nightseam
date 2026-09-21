package live_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

const environmentContract = "test/Environment"

func environmentPayload(ctx context.Context) (json.RawMessage, error) {
	owner, ok := live.OwnerOf(ctx)
	if !ok {
		return nil, errors.New("conversion lost its active owner")
	}
	ref, err := owner.Export(environmentContract, "", func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
		return raw, nil
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(ref)
}

func environmentAlias(t *testing.T, owner *live.Owner, raw json.RawMessage) live.Invoke {
	t.Helper()
	ref, err := owner.Scope().Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	alias, err := owner.Import(ref, environmentContract, "")
	if err != nil {
		t.Fatal(err)
	}
	return alias
}

func environmentCallable(t *testing.T, alias live.Invoke) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if raw, err := alias(ctx, json.RawMessage(`47`)); err != nil || string(raw) != "47" {
		t.Fatalf("retained callback: %s, %v", raw, err)
	}
}

func environmentReleased(t *testing.T, err error) {
	t.Helper()
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != live.ErrorReferenceReleased {
		t.Fatalf("want reference_released, got %v", err)
	}
}

func TestValueEnvironmentPreservesContextAndCurrentOwner(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	env := live.ValueEnvironment(p.A)
	root, explicit, foreign := p.A.Owner(), p.A.Owner().Child(), p.B.Owner()
	type key struct{}
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.WithValue(context.Background(), key{}, "operation"), deadline)
	cancel()
	check := func(t *testing.T, active context.Context) {
		t.Helper()
		got, ok := active.Deadline()
		if !ok || !got.Equal(deadline) || active.Value(key{}) != "operation" || !errors.Is(active.Err(), context.Canceled) || active.Done() != ctx.Done() {
			t.Fatal("conversion changed operation context")
		}
	}
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want *live.Owner
	}{
		{"default", ctx, root},
		{"foreign", live.WithOwner(ctx, foreign), root},
		{"explicit", live.WithOwner(ctx, explicit), explicit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected, err := env.Select(tc.ctx)
			if err != nil {
				t.Fatal(err)
			}
			check(t, selected)
			if got, _ := live.OwnerOf(selected); got != tc.want {
				t.Fatal("selection changed owner identity")
			}
			child, err := env.Child(selected)
			if err != nil {
				t.Fatal(err)
			}
			check(t, child)
			if _, err := env.Export(child, func(active context.Context) (json.RawMessage, error) {
				check(t, active)
				return json.RawMessage(`null`), nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := env.Import(child, func(active context.Context) error { check(t, active); return nil }); err != nil {
				t.Fatal(err)
			}
			if _, err := env.Publish(child, func(active context.Context) (json.RawMessage, error) {
				check(t, active)
				return json.RawMessage(`null`), nil
			}, func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil }); err != nil {
				t.Fatal(err)
			}
		})
	}

	selected, err := env.Select(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	child, err := env.Child(selected)
	if err != nil {
		t.Fatal(err)
	}
	_ = root.Release()
	fresh := p.A.Owner()
	if fresh == root {
		t.Fatal("released root was reused")
	}
	for _, ended := range []context.Context{selected, child, live.WithOwner(ctx, explicit)} {
		if _, err := env.Export(ended, environmentPayload); err == nil {
			t.Fatal("released selection switched to a fresh root")
		} else {
			environmentReleased(t, err)
		}
		if err := env.Import(ended, func(context.Context) error { t.Fatal("released import dispatched"); return nil }); err == nil {
			t.Fatal("released import succeeded")
		} else {
			environmentReleased(t, err)
		}
		_, err := env.Publish(ended, environmentPayload, func(json.RawMessage) (json.RawMessage, error) {
			t.Fatal("released publication dispatched")
			return nil, nil
		})
		environmentReleased(t, err)
	}
	for _, fallback := range []context.Context{context.Background(), live.WithOwner(context.Background(), foreign)} {
		current, err := env.Select(fallback)
		if err != nil {
			t.Fatal(err)
		}
		if owner, _ := live.OwnerOf(current); owner != fresh {
			t.Fatal("default selection retained the old root")
		}
	}
	raw, err := env.Export(context.Background(), environmentPayload)
	if err != nil {
		t.Fatal(err)
	}
	environmentCallable(t, environmentAlias(t, fresh, raw))
	if fresh.Counts() != (live.Counts{Exports: 1}) || foreign.Counts() != (live.Counts{}) {
		t.Fatal("selection allocated in the wrong owner")
	}
}

func TestValueEnvironmentNestedBoundariesKeepTheActiveBatch(t *testing.T) {
	for _, boundary := range []string{"export", "import"} {
		t.Run(boundary, func(t *testing.T) {
			p := over(t, live.Options{})
			defer p.Close()
			env, owner := live.ValueEnvironment(p.A), p.A.Owner().Child()
			ctx := live.WithOwner(context.Background(), owner)
			prior, err := environmentPayload(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var aliases []live.Invoke
			failure := errors.New("outer conversion failed")
			build := func(active context.Context) error {
				selected, err := env.Select(active)
				if err != nil {
					return err
				}
				before, _ := live.OwnerOf(active)
				after, _ := live.OwnerOf(selected)
				if before != after {
					t.Fatal("selection replaced the active batch view")
				}
				child, err := env.Child(selected)
				if err != nil {
					return err
				}
				raw, err := env.Export(child, environmentPayload)
				if err != nil {
					return err
				}
				aliases = append(aliases, environmentAlias(t, owner, raw))
				if err := env.Import(active, func(inner context.Context) error {
					raw, err := environmentPayload(inner)
					if err == nil {
						aliases = append(aliases, environmentAlias(t, owner, raw))
					}
					return err
				}); err != nil {
					return err
				}
				return failure
			}
			if boundary == "export" {
				_, err = env.Export(ctx, func(active context.Context) (json.RawMessage, error) { return nil, build(active) })
			} else {
				err = env.Import(ctx, build)
			}
			if !errors.Is(err, failure) || len(aliases) != 2 || owner.Counts() != (live.Counts{Exports: 1}) || p.A.Counts() != owner.Counts() {
				t.Fatalf("nested rollback: %v, owner=%+v scope=%+v", err, owner.Counts(), p.A.Counts())
			}
			for _, alias := range aliases {
				_, err := alias(context.Background(), nil)
				environmentReleased(t, err)
			}
			environmentCallable(t, environmentAlias(t, owner, prior))
		})
	}
}

func TestValueEnvironmentFailedImportPreservesBorrowedAttachments(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	env, owner := live.ValueEnvironment(p.A), p.A.Owner().Child()
	source := live.WithOwner(context.Background(), p.B.Owner())
	prior, err := environmentPayload(source)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := environmentPayload(source)
	if err != nil {
		t.Fatal(err)
	}
	retained := environmentAlias(t, p.A.Owner(), prior)
	var acquired live.Invoke
	failure := errors.New("later field failed")
	err = env.Import(live.WithOwner(context.Background(), owner), func(active context.Context) error {
		if err := env.Import(active, func(inner context.Context) error {
			batch, _ := live.OwnerOf(inner)
			acquired = environmentAlias(t, batch, fresh)
			environmentAlias(t, batch, prior)
			return nil
		}); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) || owner.Counts() != (live.Counts{}) || p.A.Counts() != (live.Counts{Imports: 1}) {
		t.Fatalf("failed import changed borrows: %v, %+v", err, p.A.Counts())
	}
	_, err = acquired(context.Background(), nil)
	environmentReleased(t, err)
	_ = owner.Release()
	environmentCallable(t, retained)
}

func TestValueEnvironmentPublicationRetainsOnlyUncertainAllocations(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         error
		unpublished bool
	}{
		{"proof", runtime.Unpublished(context.Canceled), true},
		{"late cancellation", context.Canceled, false},
		{"late deadline", context.DeadlineExceeded, false},
		{"remote cancellation", &runtime.PublicError{Code: "cancelled", Message: "after retention"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := over(t, live.Options{})
			defer p.Close()
			env, owner := live.ValueEnvironment(p.A), p.A.Owner().Child()
			ctx := live.WithOwner(context.Background(), owner)
			prior, err := environmentPayload(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var retained live.Invoke
			_, err = env.Publish(ctx, environmentPayload, func(raw json.RawMessage) (json.RawMessage, error) {
				if !tc.unpublished {
					retained = environmentAlias(t, p.B.Owner(), raw)
				}
				return nil, tc.err
			})
			want := live.Counts{Exports: 2}
			if tc.unpublished {
				want.Exports = 1
			}
			if !errors.Is(err, tc.err) || owner.Counts() != want || p.A.Counts() != want {
				t.Fatalf("publication changed outcome or allocations: %v, %+v", err, owner.Counts())
			}
			environmentCallable(t, environmentAlias(t, owner, prior))
			if retained != nil {
				environmentCallable(t, retained)
			}
			_ = owner.Release()
			if p.A.Counts() != (live.Counts{}) {
				t.Fatal("explicit release left publication allocations")
			}
		})
	}
}

func TestValueEnvironmentCapturedBuildContextStartsAnIndependentBatch(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	env, owner := live.ValueEnvironment(p.A), p.A.Owner().Child()
	var captured context.Context
	var later json.RawMessage
	proof := runtime.Unpublished(context.Canceled)
	_, err := env.Publish(live.WithOwner(context.Background(), owner), func(active context.Context) (json.RawMessage, error) {
		captured = active
		return environmentPayload(active)
	}, func(json.RawMessage) (json.RawMessage, error) {
		var err error
		later, err = env.Export(captured, environmentPayload)
		if err != nil {
			return nil, err
		}
		return nil, proof
	})
	if !errors.Is(err, proof) || owner.Counts() != (live.Counts{Exports: 1}) || p.A.Counts() != owner.Counts() {
		t.Fatalf("publication reclaimed later conversion: %v, %+v", err, owner.Counts())
	}
	environmentCallable(t, environmentAlias(t, owner, later))
}

func TestValueEnvironmentConcurrentPublicationsKeepSeparateContexts(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	env := live.ValueEnvironment(p.A)
	a, b := p.A.Owner().Child(), p.A.Owner().Child()
	ready, finish := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	done := make(chan error, 1)
	proof := runtime.Unpublished(context.Canceled)
	go func() {
		_, err := env.Publish(live.WithOwner(context.Background(), a), environmentPayload, func(json.RawMessage) (json.RawMessage, error) {
			close(ready)
			<-finish
			return nil, proof
		})
		done <- err
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("first publication did not reach publisher: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("first publication stalled")
	}
	raw, err := env.Publish(live.WithOwner(context.Background(), b), environmentPayload, func(raw json.RawMessage) (json.RawMessage, error) { return raw, nil })
	if err != nil {
		t.Fatal(err)
	}
	close(finish)
	select {
	case err := <-done:
		if !errors.Is(err, proof) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first publication did not settle")
	}
	if a.Counts() != (live.Counts{}) || b.Counts() != (live.Counts{Exports: 1}) || p.A.Counts() != b.Counts() {
		t.Fatalf("publication contexts interfered: A=%+v B=%+v scope=%+v", a.Counts(), b.Counts(), p.A.Counts())
	}
	_ = a.Release()
	environmentCallable(t, environmentAlias(t, b, raw))
}
