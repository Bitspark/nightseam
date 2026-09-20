package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// scopeOn is a live scope of the driver's making, with what its exported
// bindings were asked — kept per handle, as everything a testee holds is,
// and read by live.await_invocation.
type scopeOn struct {
	*live.Scope
	peer        *peer
	mu          sync.Mutex
	invocations []map[string]any
	waiting     chan struct{}
}

func (s *scopeOn) shutdown() { _ = s.Close() }

// took records that an exported binding was asked something, for a scenario to
// read. It records the contract and the outcome, never the request.
func (s *scopeOn) took(contract, outcome string) {
	s.mu.Lock()
	s.invocations = append(s.invocations, map[string]any{"contract": contract, "outcome": outcome})
	wake := s.waiting
	s.waiting = nil
	s.mu.Unlock()
	if wake != nil {
		close(wake)
	}
}

func (s *scopeOn) take(contract string, within time.Duration) (map[string]any, bool) {
	deadline := time.After(within)
	for {
		s.mu.Lock()
		for i, one := range s.invocations {
			if contract == "" || one["contract"] == contract {
				s.invocations = append(s.invocations[:i], s.invocations[i+1:]...)
				s.mu.Unlock()
				return one, true
			}
		}
		if s.waiting == nil {
			s.waiting = make(chan struct{})
		}
		wake := s.waiting
		s.mu.Unlock()
		select {
		case <-wake:
		case <-deadline:
			return nil, false
		}
	}
}

// bindingOn is one imported binding: the function to call it with, and the
// scope it belongs to, for the error mapping.
type bindingOn struct {
	invoke live.Invoke
	scope  *scopeOn
}

type ownerOn struct {
	owner *live.Owner
	scope *scopeOn
}

func (t *testee) ownerOf(r request, s *scopeOn) (*live.Owner, error) {
	name, err := r.string("owner")
	if err != nil {
		return nil, err
	}
	if name == "" {
		return s.Owner(), nil
	}
	object, ok := t.lookup(name)
	if !ok {
		return nil, fail("unknown_handle", "%s", name)
	}
	o, ok := object.(*ownerOn)
	if !ok || o.scope != s {
		return nil, invalid("%s is not an owner of this scope", name)
	}
	return o.owner, nil
}

func (t *testee) ownerHandle(r request) (*ownerOn, error) {
	name, err := r.mustString("on")
	if err != nil {
		return nil, err
	}
	object, ok := t.lookup(name)
	if !ok {
		return nil, fail("unknown_handle", "%s", name)
	}
	o, ok := object.(*ownerOn)
	if !ok {
		return nil, invalid("%s is not a live owner", name)
	}
	return o, nil
}

func (t *testee) scopeOf(r request, name string) (*scopeOn, error) {
	handle, err := r.mustString(name)
	if err != nil {
		return nil, err
	}
	object, ok := t.lookup(handle)
	if !ok {
		return nil, fail("unknown_handle", "%s", handle)
	}
	s, ok := object.(*scopeOn)
	if !ok {
		return nil, invalid("%s is not a live scope", handle)
	}
	return s, nil
}

func (t *testee) bindingOf(r request, name string) (*bindingOn, error) {
	handle, err := r.mustString(name)
	if err != nil {
		return nil, err
	}
	object, ok := t.lookup(handle)
	if !ok {
		return nil, fail("unknown_handle", "%s", handle)
	}
	a, ok := object.(*bindingOn)
	if !ok {
		return nil, invalid("%s is not a live attachment", handle)
	}
	return a, nil
}

// reference reads the reference a step carries, which is the value the other
// side's export answered with and the scenario passed along: a scope decodes
// it, since a reference of no scope is not a reference at all.
func (t *testee) reference(r request, s *scopeOn, name string) (live.Reference, error) {
	raw := r.raw(name)
	if raw == nil {
		return live.Reference{}, invalid("%s names no reference", name)
	}
	ref, err := s.Decode(raw)
	if err != nil {
		return live.Reference{}, liveError(err)
	}
	return ref, nil
}

// cannedInvoke is what an exported binding does, from the same canned set a
// peer's handler takes: the testee is a peer under control, not a script.
func cannedInvoke(t *testee, s *scopeOn, contract string, b behavior) live.Invoke {
	return func(ctx context.Context, request json.RawMessage) (json.RawMessage, error) {
		switch b.Kind {
		case "", "echo":
			s.took(contract, "ok")
			if request == nil {
				return json.RawMessage("null"), nil
			}
			return request, nil
		case "return":
			s.took(contract, "ok")
			if b.Value == nil {
				return json.RawMessage("null"), nil
			}
			return b.Value, nil
		case "through":
			// A binding that calls another binding: what a callable returned
			// by one call does when it reaches a callable supplied by it.
			object, ok := t.lookup(b.Attachment)
			if !ok {
				return nil, invalid("%s is not an attachment", b.Attachment)
			}
			a, ok := object.(*bindingOn)
			if !ok {
				return nil, invalid("%s is not an attachment", b.Attachment)
			}
			result, err := a.invoke(ctx, request)
			if err != nil {
				s.took(contract, "error")
				return nil, err
			}
			s.took(contract, "ok")
			return result, nil
		case "fail":
			s.took(contract, "error")
			return nil, &runtime.PublicError{Code: b.Code, Message: b.Message, Data: b.Data}
		case "wait":
			s.took(contract, "started")
			<-ctx.Done()
			s.took(contract, "cancelled")
			return nil, ctx.Err()
		case "hold":
			// Held until the remote emits what releases it, its own
			// cancellation notwithstanding: this is how a scenario holds
			// *when* a withdrawn invocation settles. The one binding that
			// does not stop when it is told to, as the peer's hold is.
			released := make(chan struct{}, 1)
			unsubscribe := s.peer.Peer.OnEvent(func(_ context.Context, e runtime.Event) {
				if e.Name == b.Until {
					select {
					case released <- struct{}{}:
					default:
					}
				}
			})
			defer unsubscribe()
			s.took(contract, "started")
			select {
			case <-released:
			case <-time.After(30 * time.Second):
			}
			s.took(contract, "ok")
			if b.Value == nil {
				return json.RawMessage("null"), nil
			}
			return b.Value, nil
		}
		return nil, invalid("a binding echoes, returns, calls through, fails, waits or holds")
	}
}

// liveError maps the layer's refusals onto the driver's answer: a public code
// crosses as itself, as every refusal of this layer is one.
func liveError(err error) *failure {
	var public *runtime.PublicError
	if errors.As(err, &public) {
		return fail(public.Code, "%s", public.Message)
	}
	return fail("failed", "%v", err)
}

func (t *testee) liveOps() map[string]func(request) (any, error) {
	return map[string]func(request) (any, error){
		"live.owner": func(r request) (any, error) {
			s, err := t.scopeOf(r, "on")
			if err != nil {
				return nil, err
			}
			o, err := t.ownerOf(r, s)
			if err != nil {
				return nil, err
			}
			root, err := r.bool("root")
			if err != nil {
				return nil, err
			}
			if !root {
				o = o.Child()
			}
			return map[string]any{"handle": t.mint("owner", &ownerOn{owner: o, scope: s})}, nil
		},
		"live.owner_release": func(r request) (any, error) {
			o, err := t.ownerHandle(r)
			if err != nil {
				return nil, err
			}
			if err := o.owner.Release(); err != nil {
				return nil, liveError(err)
			}
			return nil, nil
		},
		"live.owner_counts": func(r request) (any, error) {
			o, err := t.ownerHandle(r)
			if err != nil {
				return nil, err
			}
			c := o.owner.Counts()
			return map[string]any{"exports": c.Exports, "imports": c.Imports}, nil
		},
		"live.import_value": func(r request) (any, error) {
			s, err := t.scopeOf(r, "on")
			if err != nil {
				return nil, err
			}
			o, err := t.ownerOf(r, s)
			if err != nil {
				return nil, err
			}
			contract, err := r.string("contract")
			if err != nil {
				return nil, err
			}
			var references []json.RawMessage
			if err := json.Unmarshal(r.raw("references"), &references); err != nil {
				return nil, invalid("references: %v", err)
			}
			abort, err := r.bool("fail")
			if err != nil {
				return nil, err
			}
			var imports []live.Invoke
			err = o.ImportValue(func(batch *live.Owner) error {
				for _, raw := range references {
					ref, err := s.Decode(raw)
					if err != nil {
						return err
					}
					fn, err := batch.Import(ref, contract)
					if err != nil {
						return err
					}
					imports = append(imports, fn)
				}
				if abort {
					return &runtime.PublicError{Code: "fixture_failed", Message: "failed after imports"}
				}
				return nil
			})
			if err != nil {
				return nil, liveError(err)
			}
			handles := make([]string, len(imports))
			for i, fn := range imports {
				handles[i] = t.mint("at", &bindingOn{invoke: fn, scope: s})
			}
			return map[string]any{"handles": handles}, nil
		},
		"live.over": func(r request) (any, error) {
			p, err := t.peerOf(r, "on")
			if err != nil {
				return nil, err
			}
			options, err := r.object("options")
			if err != nil {
				return nil, err
			}
			var o live.Options
			if raw, ok := options["max_exports"]; ok {
				var n int
				if err := json.Unmarshal(raw, &n); err != nil {
					return nil, invalid("max_exports: %v", err)
				}
				o.MaxExports = n
			}
			if raw, ok := options["max_imports"]; ok {
				var n int
				if err := json.Unmarshal(raw, &n); err != nil {
					return nil, invalid("max_imports: %v", err)
				}
				o.MaxImports = n
			}
			scope, err := live.Over(p.Peer, o)
			if err != nil {
				return nil, invalid("%v", err)
			}
			return map[string]any{"handle": t.mint("lv", &scopeOn{Scope: scope, peer: p})}, nil
		},
		"live.export": func(r request) (any, error) {
			s, err := t.scopeOf(r, "on")
			if err != nil {
				return nil, err
			}
			contract, err := r.string("contract")
			if err != nil {
				return nil, err
			}
			b, err := parseBehavior(r.raw("behavior"))
			if err != nil {
				return nil, err
			}
			o, err := t.ownerOf(r, s)
			if err != nil {
				return nil, err
			}
			ref, err := o.Export(contract, cannedInvoke(t, s, contract, b))
			if err != nil {
				return nil, liveError(err)
			}
			carried, err := json.Marshal(ref)
			if err != nil {
				return nil, fail("failed", "%v", err)
			}
			var reference any
			if err := json.Unmarshal(carried, &reference); err != nil {
				return nil, fail("failed", "%v", err)
			}
			return map[string]any{"reference": reference}, nil
		},
		"live.import": func(r request) (any, error) {
			s, err := t.scopeOf(r, "on")
			if err != nil {
				return nil, err
			}
			contract, err := r.string("contract")
			if err != nil {
				return nil, err
			}
			ref, err := t.reference(r, s, "reference")
			if err != nil {
				return nil, err
			}
			o, err := t.ownerOf(r, s)
			if err != nil {
				return nil, err
			}
			invoke, err := o.Import(ref, contract)
			if err != nil {
				return nil, liveError(err)
			}
			return map[string]any{"handle": t.mint("at", &bindingOn{invoke: invoke, scope: s})}, nil
		},
		"live.invoke": func(r request) (any, error) {
			a, err := t.bindingOf(r, "on")
			if err != nil {
				return nil, err
			}
			timeout, err := r.int("timeout_ms", 0)
			if err != nil {
				return nil, err
			}
			cancelled, err := r.bool("cancelled")
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithCancel(context.Background())
			if timeout > 0 {
				ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
			}
			if cancelled {
				cancel()
			}
			request := r.raw("request")
			// An invocation is a call like any other: it answers with a call
			// handle, and call.await and call.cancel act on it.
			c := &call{cancel: cancel, done: make(chan struct{})}
			go func() {
				defer close(c.done)
				c.result, c.err = a.invoke(ctx, request)
			}()
			return map[string]any{"handle": t.mint("call", &callOn{call: c, peer: a.scope.peer})}, nil
		},
		"live.release": func(r request) (any, error) {
			s, err := t.scopeOf(r, "on")
			if err != nil {
				return nil, err
			}
			ref, err := t.reference(r, s, "reference")
			if err != nil {
				return nil, err
			}
			if err := s.Release(ref); err != nil {
				return nil, liveError(err)
			}
			return nil, nil
		},
		"live.forward": func(r request) (any, error) {
			s, err := t.scopeOf(r, "on")
			if err != nil {
				return nil, err
			}
			contract, err := r.string("contract")
			if err != nil {
				return nil, err
			}
			a, err := t.bindingOf(r, "attachment")
			if err != nil {
				return nil, err
			}
			o, err := t.ownerOf(r, s)
			if err != nil {
				return nil, err
			}
			ref, err := live.Forward(o, contract, a.invoke)
			if err != nil {
				return nil, liveError(err)
			}
			carried, err := json.Marshal(ref)
			if err != nil {
				return nil, fail("failed", "%v", err)
			}
			var reference any
			if err := json.Unmarshal(carried, &reference); err != nil {
				return nil, fail("failed", "%v", err)
			}
			return map[string]any{"reference": reference}, nil
		},
		"live.counts": func(r request) (any, error) {
			s, err := t.scopeOf(r, "on")
			if err != nil {
				return nil, err
			}
			counts := s.Counts()
			return map[string]any{"exports": counts.Exports, "imports": counts.Imports}, nil
		},
		"live.await_invocation": func(r request) (any, error) {
			s, err := t.scopeOf(r, "on")
			if err != nil {
				return nil, err
			}
			contract, err := r.string("contract")
			if err != nil {
				return nil, err
			}
			within, err := r.within()
			if err != nil {
				return nil, err
			}
			one, ok := s.take(contract, within)
			if !ok {
				return nil, fail("timeout", "no invocation within %s", within)
			}
			return one, nil
		},
		"live.close": func(r request) (any, error) {
			s, err := t.scopeOf(r, "on")
			if err != nil {
				return nil, err
			}
			return nil, s.Close()
		},
	}
}
