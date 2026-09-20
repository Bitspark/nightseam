package live_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

func TestOwnerContextCarriesTheExplicitLifetime(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	owner := p.A.Owner().Child()
	if _, ok := live.OwnerOf(context.Background()); ok {
		t.Fatal("missing owner found")
	}
	ctx := live.WithOwner(context.Background(), owner)
	if got, ok := live.OwnerOf(ctx); !ok || got != owner {
		t.Fatal("owner context changed identity")
	}
	if _, ok := live.OwnerOf(live.WithOwner(ctx, nil)); ok {
		t.Fatal("nil owner overrides the root default")
	}
}

func TestImportValueBoundFailurePreservesPriorAttachments(t *testing.T) {
	p := over(t, live.Options{MaxImports: 2})
	defer p.Close()
	echo := func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) { return raw, nil }
	var refs []live.Reference
	for range 3 {
		ref, err := p.A.Owner().Export("test/Call", echo)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(ref)
		arrived, err := p.B.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, arrived)
	}
	retained, err := p.B.Owner().Import(refs[0], "test/Call")
	if err != nil {
		t.Fatal(err)
	}
	owner := p.B.Owner().Child()
	var fresh live.Invoke
	err = owner.ImportValue(func(batch *live.Owner) error {
		fresh, err = batch.Import(refs[1], "test/Call")
		if err != nil {
			return err
		}
		if _, err := batch.Import(refs[0], "test/Call"); err != nil {
			return err
		}
		_, err := batch.Import(refs[2], "test/Call")
		return err
	})
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != live.ErrorTooManyImports {
		t.Fatalf("limit failure: %v", err)
	}
	if got := p.B.Counts(); got.Imports != 1 || owner.Counts() != (live.Counts{}) {
		t.Fatalf("partial walk leaked: %+v %+v", got, owner.Counts())
	}
	if _, err := fresh(context.Background(), nil); !errors.As(err, &public) || public.Code != live.ErrorReferenceReleased {
		t.Fatalf("new attachment survived: %v", err)
	}
	if value, err := retained(context.Background(), json.RawMessage("7")); err != nil || string(value) != "7" {
		t.Fatalf("retained alias: %s %v", value, err)
	}
	_ = p.A.Owner().Release()
	_ = p.B.Owner().Release()
}

type ownerObserver func(runtime.ObserverEvent)

func (f ownerObserver) Observe(event runtime.ObserverEvent) { f(event) }

func TestOwnerReleaseRevokesAllAliasesBeforeObserverReentry(t *testing.T) {
	var owner *live.Owner
	var aliases []live.Invoke
	var reentries int
	seen := &recorder{}
	p := overObserved(t, live.Options{}, ownerObserver(func(event runtime.ObserverEvent) {
		seen.Observe(event)
		if _, ok := event.(live.LiveReleased); !ok {
			return
		}
		reentries++
		if err := owner.Release(); err != nil {
			t.Error(err)
		}
		for _, alias := range aliases {
			_, err := alias(context.Background(), nil)
			var public *runtime.PublicError
			if !errors.As(err, &public) || public.Code != live.ErrorReferenceReleased {
				t.Errorf("reentrant alias survived: %v", err)
			}
		}
	}))
	defer p.Close()
	owner = p.A.Owner().Child()
	for range 3 {
		ref, err := owner.Child().Export("test/Call", func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, nil })
		if err != nil {
			t.Fatal(err)
		}
		alias, err := p.A.Owner().Import(ref, "test/Call")
		if err != nil {
			t.Fatal(err)
		}
		aliases = append(aliases, alias)
	}
	if err := owner.Release(); err != nil {
		t.Fatal(err)
	}
	if reentries != 3 {
		t.Fatalf("release notifications: %d", reentries)
	}
	emitted := 0
	for _, event := range seen.events() {
		if e, ok := event.(runtime.EventEmitted); ok && e.Name == live.ReleaseEvent {
			emitted++
		}
	}
	if emitted != 3 {
		t.Fatalf("release events: %d", emitted)
	}
	if p.A.Counts() != (live.Counts{}) {
		t.Fatal(p.A.Counts())
	}
}

func TestUnpublishedExportRollbackEmitsNoRelease(t *testing.T) {
	seen := &recorder{}
	p := overObserved(t, live.Options{}, seen)
	defer p.Close()
	owner := p.A.Owner().Child()
	_, err := owner.ExportValue(func(batch *live.Owner) (json.RawMessage, error) {
		if _, err := batch.Export("test/Call", func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, nil }); err != nil {
			return nil, err
		}
		return nil, errors.New("unpublished")
	})
	if err == nil {
		t.Fatal("failed build succeeded")
	}
	_ = owner.Release()
	for _, event := range seen.events() {
		if e, ok := event.(runtime.EventEmitted); ok && e.Name == live.ReleaseEvent {
			t.Fatal("rollback published live.release")
		}
	}
	if owner.Counts() != (live.Counts{}) || p.A.Counts() != (live.Counts{}) {
		t.Fatal("rollback retained allocations")
	}
}

func TestConcurrentOwnerReleaseAndAcquisition(t *testing.T) {
	p := over(t, live.Options{})
	defer p.Close()
	for range 32 {
		owner := p.A.Owner().Child()
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				child := owner.Child()
				ref, err := child.Export("test/Call", func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, nil })
				if err == nil {
					_, _ = p.A.Owner().Import(ref, "test/Call")
				}
				_ = owner.Release()
			}()
		}
		wg.Wait()
		if owner.Counts() != (live.Counts{}) || p.A.Counts() != (live.Counts{}) {
			t.Fatal("concurrent release retained bindings")
		}
	}
}

func TestImportRollbackDoesNotRepeatAnAlreadyReleasedAllocation(t *testing.T) {
	seen := &recorder{}
	p := overObserved(t, live.Options{MaxImports: 1, MaxExports: 1}, seen)
	defer p.Close()
	owner := p.A.Owner().Child()
	sibling := p.A.Owner().Child()
	var first live.Reference
	var replacement live.Invoke
	err := owner.ImportValue(func(batch *live.Owner) error {
		// Import need not prove a binding exists remotely; these references exercise
		// attachment bookkeeping without depending on remote event delivery speed.
		for i := range 8 {
			raw, _ := json.Marshal(map[string]string{"binding": fmt.Sprintf("remote.%d", i), "contract": "test/Call"})
			ref, err := p.A.Decode(raw)
			if err != nil {
				return err
			}
			if i == 0 {
				first = ref
			}
			if _, err := batch.Import(ref, "test/Call"); err != nil {
				return err
			}
			if err := p.A.Release(ref); err != nil {
				return err
			}
		}
		// Its tombstone is gone, so the same binding ID now has a distinct
		// attachment owned outside this batch. Rollback must not revoke it.
		var err error
		replacement, err = sibling.Import(first, "test/Call")
		if err != nil {
			return err
		}
		return errors.New("later failure")
	})
	if err == nil || err.Error() != "later failure" {
		t.Fatalf("unexpected import outcome: %v", err)
	}
	_ = owner.Release()
	emitted := 0
	for _, event := range seen.events() {
		if e, ok := event.(runtime.EventEmitted); ok && e.Name == live.ReleaseEvent {
			emitted++
		}
	}
	if emitted != 8 {
		t.Fatalf("each allocation needs exactly one release; got %d events", emitted)
	}
	if owner.Counts() != (live.Counts{}) || sibling.Counts() != (live.Counts{Imports: 1}) || p.A.Counts() != (live.Counts{Imports: 1}) {
		t.Fatalf("rollback changed sibling allocation: owner=%+v sibling=%+v scope=%+v", owner.Counts(), sibling.Counts(), p.A.Counts())
	}
	_, err = replacement(context.Background(), nil)
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != live.ErrorReferenceUnknown {
		t.Fatalf("replacement attachment was revoked: %v", err)
	}
	_ = sibling.Release()
	if p.A.Counts() != (live.Counts{}) {
		t.Fatal("explicit sibling release retained an attachment")
	}
}
