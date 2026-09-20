package live

import (
	"encoding/json"
	"errors"

	"github.com/Bitspark/nightseam/runtime/go"
)

type allocation struct {
	id       string
	imported bool
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
	value, _, err = o.buildExport(build)
	return value, err
}

// PublishValue builds a complete payload, then attempts to publish it. Only a
// local UnpublishedError unwinds this build's new allocations. Timeouts,
// cancellation after dispatch, remote errors and lost replies retain them under
// the owner until explicit release or scope end. The build view is inactive
// before publish runs, so later conversions start independent batches.
func (o *Owner) PublishValue(build func(*Owner) (json.RawMessage, error), publish func(json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error) {
	value, allocations, err := o.buildExport(build)
	if err != nil {
		return nil, err
	}
	result, err := publish(value)
	var proof *runtime.UnpublishedError
	if errors.As(err, &proof) {
		o.discard(allocations)
	}
	return result, err
}

func (o *Owner) buildExport(build func(*Owner) (json.RawMessage, error)) (value json.RawMessage, allocations []allocation, err error) {
	view, err := o.begin()
	if err != nil {
		return nil, nil, err
	}
	committed := false
	defer func() { allocations = view.finish(committed) }()
	value, err = build(view)
	if err != nil {
		return nil, nil, err
	}
	if value != nil && !json.Valid(value) {
		return nil, nil, errors.New("a live export must produce valid JSON")
	}
	committed = true
	return value, nil, nil
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

func (o *Owner) finish(committed bool) []allocation {
	s := o.Scope()
	s.mu.Lock()
	batch := o.batch
	allocations := batch.allocations
	if committed && batch.parent != nil && batch.parent.active {
		batch.parent.allocations = append(batch.parent.allocations, allocations...)
	}
	batch.active, batch.allocations, batch.parent = false, nil, nil
	s.mu.Unlock()
	if !committed {
		o.discard(allocations)
	}
	return allocations
}

func (o *Owner) discard(allocations []allocation) {
	s := o.Scope()
	s.mu.Lock()
	var notices []releaseNotice
	for _, allocation := range allocations {
		// Explicit release or a remote notification may already have
		// removed this allocation. A bounded tombstone is not its lifetime.
		if s.exports[allocation.id] == nil && s.imports[allocation.id] == nil {
			continue
		}
		notices = append(notices, s.takeRelease(allocation.id, allocation.imported))
	}
	s.mu.Unlock()
	for _, notice := range notices {
		s.notifyRelease(notice)
	}
}
