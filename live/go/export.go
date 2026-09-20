package live

import (
	"encoding/json"
	"errors"
)

type allocation struct {
	id         string
	imported   bool
	binding    *binding
	attachment *attachment
}

// current compares the allocation itself, not merely its reusable binding ID.
// Callers hold the scope mutex while checking and revoking an allocation.
func (a allocation) current(s *Scope) bool {
	if a.imported {
		return a.attachment != nil && s.imports[a.id] == a.attachment
	}
	return a.binding != nil && s.exports[a.id] == a.binding
}

// Completed views retain owner identity, but no batch allocation history.
// The enclosing scope mutex protects active batches as well as owner state.
type valueBatch struct {
	parent      *valueBatch
	active      bool
	allocations []allocation
}

// ExportValue constructs an unpublished JSON payload. The synchronous build
// must use its supplied owner view and must not publish partial values. Errors,
// invalid JSON and panics revoke only bindings newly allocated by this build.
// Nested successful builds join their parent. Completed views can be captured
// by callable implementations and start fresh conversions later.
//
// A successful build commits its allocations to the owner. A later RPC failure
// does not roll them back. Unpublished exports are discarded without a release
// event; new import attachments are released with the ordinary notification.
func (o *Owner) ExportValue(build func(*Owner) (json.RawMessage, error)) (value json.RawMessage, err error) {
	view, err := o.begin()
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() { view.finish(committed) }()
	value, err = build(view)
	if err != nil {
		return nil, err
	}
	if value != nil && !json.Valid(value) {
		return nil, errors.New("a live export must produce valid JSON")
	}
	committed = true
	return value, nil
}

// ImportValue walks a received value synchronously under a batch owner view.
// An error or panic releases only newly attached bindings and new exports;
// existing aliases are borrows and survive a failed walk.
func (o *Owner) ImportValue(build func(*Owner) error) (err error) {
	view, err := o.begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() { view.finish(committed) }()
	err = build(view)
	committed = err == nil
	return err
}

func (o *Owner) begin() (*Owner, error) {
	s := o.Scope()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, s.refuse("", ErrorScopeClosed, "the scope ended")
	}
	if o.state.ended() {
		s.mu.Unlock()
		return nil, s.refuse("", ErrorReferenceReleased, "the owner was released")
	}
	batch := &valueBatch{active: true}
	if o.batch != nil && o.batch.active {
		batch.parent = o.batch
	}
	s.mu.Unlock()
	return &Owner{state: o.state, batch: batch}, nil
}

func (o *Owner) finish(committed bool) {
	s := o.Scope()
	s.mu.Lock()
	batch := o.batch
	allocations := batch.allocations
	if committed && batch.parent != nil && batch.parent.active {
		batch.parent.allocations = append(batch.parent.allocations, allocations...)
	}
	batch.active, batch.allocations, batch.parent = false, nil, nil
	var notices []releaseNotice
	if !committed {
		for _, allocation := range allocations {
			// A release may have removed this allocation, and tombstone
			// eviction can let another owner attach to the same binding ID.
			if !allocation.current(s) {
				continue
			}
			notices = append(notices, s.takeRelease(allocation.id, allocation.imported))
		}
	}
	s.mu.Unlock()
	for _, notice := range notices {
		s.notifyRelease(notice)
	}
}
