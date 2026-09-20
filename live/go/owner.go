package live

import "context"

// Owner is a caller-chosen lifetime inside one scope. It owns bindings it
// creates, and borrows existing attachments without extending their lifetime.
// Releasing it revokes its bindings and descendants, never its borrows.
type Owner struct {
	state *ownerState
	batch *valueBatch
}

// All owner state is protected by the scope mutex. Empty descendants are not
// retained by their parent; their ancestor chain still makes release terminal.
type ownerState struct {
	scope    *Scope
	parent   *ownerState
	children map[*ownerState]struct{}
	owned    map[string]bool // true for an import, false for an export
	released bool
}

// Owner returns the scope's root lifetime. After that lifetime is released,
// the still-open scope supplies a fresh root; old owners remain released.
func (s *Scope) Owner() *Owner {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owner == nil || s.owner.state.released {
		s.owner = &Owner{state: &ownerState{scope: s.root}}
	}
	return s.owner
}

// Child makes a nested lifetime. Releasing its parent also releases it.
func (o *Owner) Child() *Owner {
	return &Owner{state: &ownerState{scope: o.Scope(), parent: o.state}, batch: o.batch}
}

// Scope is the connection scope this lifetime belongs to.
func (o *Owner) Scope() *Scope { return o.state.scope }

// Counts reports this owner's direct allocations, excluding borrows and children.
func (o *Owner) Counts() Counts {
	s := o.Scope()
	s.mu.Lock()
	defer s.mu.Unlock()
	var counts Counts
	for _, imported := range o.state.owned {
		if imported {
			counts.Imports++
		} else {
			counts.Exports++
		}
	}
	return counts
}

// Release revokes this owner's bindings, children first, once. It leaves the
// scope open and lets invocations already dispatched settle normally.
func (o *Owner) Release() error {
	s := o.Scope()
	s.mu.Lock()
	ids := o.state.end()
	notices := make([]releaseNotice, 0, len(ids))
	for _, id := range ids {
		notices = append(notices, s.takeRelease(id, true))
	}
	s.mu.Unlock()
	for _, notice := range notices {
		s.notifyRelease(notice)
	}
	return nil
}

func (o *ownerState) ended() bool {
	for current := o; current != nil; current = current.parent {
		if current.released {
			return true
		}
	}
	return false
}

// end invalidates the tree under the lock; notification runs after unlocking.
func (o *ownerState) end() []string {
	if o.released {
		return nil
	}
	o.released = true
	var ids []string
	for child := range o.children {
		ids = append(ids, child.end()...)
	}
	for id := range o.owned {
		ids = append(ids, id)
	}
	o.owned, o.children = nil, nil
	o.prune()
	return ids
}

func (o *Owner) record(id string, imported bool) {
	if o.state.owned == nil {
		o.state.owned = map[string]bool{}
	}
	o.state.owned[id] = imported
	for child := o.state; child.parent != nil; child = child.parent {
		if child.parent.children == nil {
			child.parent.children = map[*ownerState]struct{}{}
		}
		child.parent.children[child] = struct{}{}
	}
	if o.batch != nil && o.batch.active {
		s := o.Scope()
		o.batch.allocations = append(o.batch.allocations, allocation{
			id: id, imported: imported, binding: s.exports[id], attachment: s.imports[id],
		})
	}
}

func (o *ownerState) forget(id string) {
	delete(o.owned, id)
	o.prune()
}

func (o *ownerState) prune() {
	for child := o; child.parent != nil && len(child.owned) == 0 && len(child.children) == 0; child = child.parent {
		delete(child.parent.children, child)
	}
}

type ownerContextKey struct{}

// WithOwner carries the caller-selected lifetime through a generated operation.
func WithOwner(ctx context.Context, o *Owner) context.Context {
	return context.WithValue(ctx, ownerContextKey{}, o)
}

// OwnerOf finds the explicit lifetime carried by this context, if any.
func OwnerOf(ctx context.Context) (*Owner, bool) {
	o, ok := ctx.Value(ownerContextKey{}).(*Owner)
	return o, ok && o != nil
}
