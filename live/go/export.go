package live

import (
	"encoding/json"
	"errors"
)

// exportBatch is protected by its scope's mutex. Completed views may be
// captured by callable implementations, but retain no allocation history.
type exportBatch struct {
	parent *exportBatch
	active bool
	ids    []string
}

// ExportValue constructs an unpublished JSON payload. build must finish all
// conversion synchronously using the supplied view of this scope, and must not
// publish partial values. An error, invalid JSON or panic discards only exports
// allocated through that view. Nested successful builds join the enclosing
// build; success at the outer boundary commits the allocations.
//
// The view has this scope's identity. A later conversion through a captured view
// starts a fresh build. This is not rollback for a failed RPC: once build has
// succeeded, publication and ownership are the caller's responsibility.
func (s *Scope) ExportValue(build func(*Scope) (json.RawMessage, error)) (value json.RawMessage, err error) {
	batch := &exportBatch{active: true}
	s.mu.Lock()
	if s.batch != nil && s.batch.active {
		batch.parent = s.batch
	}
	s.mu.Unlock()
	view := &Scope{scopeState: s.scopeState, batch: batch}
	committed := false
	defer func() {
		s.mu.Lock()
		ids := batch.ids
		if committed && batch.parent != nil && batch.parent.active {
			batch.parent.ids = append(batch.parent.ids, ids...)
		}
		batch.active, batch.ids, batch.parent = false, nil, nil
		s.mu.Unlock()
		if !committed {
			for _, id := range ids {
				s.release(id, false)
			}
		}
	}()
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
