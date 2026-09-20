// Package live carries callable values across one connection: a scope over a
// peer, bindings exported from it, and references that name them inside an
// ordinary payload. It is the third level of the declaration language — data,
// RPC, live — and the runtime half of it.
//
// A callable is one unary function, so a binding is one function made
// addressable from the other side of one connection. The layer speaks as a
// layer speaks: a reserved prefix, live., and ordinary frames of the profile
// beneath — live.invoke, a request, and live.release, an event — exactly as the
// tunnel speaks channel.open. Nothing of this reaches the envelope, no frame
// kind is added, and the peer acts on nothing it did not act on before.
//
// What a reference names is a binding of one scope, and a scope is one
// connection. A scope mints a random nonce to give its bindings a fresh
// namespace. Invocation looks up the binding in the exporting scope's current
// table; reconnection does not restore the previous scope's exports. A native
// Reference carries the scope that minted it through Export or Scope.Decode.
// Its serialized bytes can be decoded and imported again; decoding associates
// them with the receiving scope without proving their provenance or that the
// named binding exists.
//
// The layer proves which binding of which contract, and never who may call it.
// Application authorization is the application's, above this.
package live

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/Bitspark/nightseam/runtime/go"
)

// The profile's operations of the live layer.
const (
	InvokeMethod = "live.invoke"
	ReleaseEvent = "live.release"
)

// The public errors this layer refuses with. They are the same names in every
// language; a scope proves the binding and the contract, and says which of
// them it could not prove.
const (
	// ErrorContractInvalid is an export or an import of no contract at all.
	ErrorContractInvalid = "contract_invalid"
	// ErrorContractMismatch is a reference whose contract is not the one
	// expected where it arrived, or whose invocation names another.
	ErrorContractMismatch = "contract_mismatch"
	// ErrorReferenceUnknown is an invocation naming no current export in this
	// scope, including a binding id carried from an earlier connection.
	ErrorReferenceUnknown = "reference_unknown"
	// ErrorReferenceForeign is a native reference associated with another
	// scope, refused here before it reaches the wire.
	ErrorReferenceForeign = "reference_foreign"
	// ErrorReferenceReleased is an invocation of a binding that was released.
	ErrorReferenceReleased = "reference_released"
	// ErrorScopeClosed is anything at all after the scope ended.
	ErrorScopeClosed = "scope_closed"
	// ErrorTooManyExports and ErrorTooManyImports are the scope's bounds.
	ErrorTooManyExports = "too_many_exports"
	ErrorTooManyImports = "too_many_imports"
)

// Invoke is what a binding is: a request in, a result out. A nil request is
// the callable that takes nothing; a nil result is the callable that returns
// nothing. A *runtime.PublicError crosses the wire with its code, as it does
// from any handler; any other error reaches the caller as internal.
type Invoke func(ctx context.Context, request json.RawMessage) (json.RawMessage, error)

// Options bound a scope. Zero values select the defaults.
type Options struct {
	// MaxExports bounds the bindings this side may have exported at once; the
	// one past it is refused too_many_exports and registers nothing. Zero: 1024.
	MaxExports int
	// MaxImports bounds the bindings this side may hold an attachment to.
	// Zero: 1024.
	MaxImports int
}

func (o Options) normalized() (Options, error) {
	if o.MaxExports < 0 || o.MaxImports < 0 {
		return o, errors.New("live limits must not be negative")
	}
	if o.MaxExports == 0 {
		o.MaxExports = 1024
	}
	if o.MaxImports == 0 {
		o.MaxImports = 1024
	}
	return o, nil
}

// Counts is what a scope holds, which is what a scenario asserts is nothing
// once an exchange is over: a binding nobody released is a leak, and this is
// how the suite sees one.
type Counts struct {
	Exports int
	Imports int
}

// Reference names one binding of one scope. It has no exported field and no
// public constructor: it is minted by Export or by Scope.Decode, carries the
// scope it was minted in, and is refused reference_foreign in another scope.
// MarshalJSON exposes its binding and contract without that native association;
// decoding those bytes creates a reference associated with the decoding scope.
type Reference struct {
	binding  string
	contract string
	scope    *Scope
}

// Contract is the declared contract the reference carries.
func (r Reference) Contract() string { return r.contract }

// MarshalJSON writes the binding and contract as an ordinary payload value.
// It does not serialize the native reference's scope association.
func (r Reference) MarshalJSON() ([]byte, error) {
	if r.binding == "" {
		return nil, errors.New("a live reference names no binding")
	}
	return json.Marshal(referenceWire{Binding: r.binding, Contract: r.contract})
}

type referenceWire struct {
	Binding  string `json:"binding"`
	Contract string `json:"contract"`
}

type binding struct {
	contract string
	invoke   Invoke
	released bool
}

type attachment struct {
	contract string
	invoke   Invoke
	released bool
}

// Scope is the live layer over one peer: what this side has exported over that
// connection, what it has imported over it, and nothing that outlives it.
type Scope struct {
	*scopeState
	batch *exportBatch
}

// Every conversion view shares the connection's state and canonical identity.
type scopeState struct {
	root    *Scope
	peer    *runtime.Peer
	options Options
	nonce   string

	mu       sync.Mutex
	next     int64
	closed   bool
	exports  map[string]*binding
	imports  map[string]*attachment
	gone     map[string]struct{}
	order    []string
	inflight map[int64]context.CancelFunc
	call     int64
}

var scopes sync.Map // *runtime.Peer -> *Scope

// Over makes the live layer over a peer, registering its operations on it; a
// peer carries one scope. Make it where a tunnel is made — in
// runtime.Options.Prepare, before the peer reads its first frame — since a peer
// already reading can refuse the other side's first live.invoke
// method_not_found before the handler is there.
func Over(peer *runtime.Peer, options Options) (*Scope, error) {
	if peer == nil {
		return nil, errors.New("a live scope needs a peer")
	}
	o, err := options.normalized()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("a live scope needs a nonce: %w", err)
	}
	s := &Scope{scopeState: &scopeState{
		peer:     peer,
		options:  o,
		nonce:    hex.EncodeToString(nonce),
		next:     1,
		exports:  map[string]*binding{},
		imports:  map[string]*attachment{},
		gone:     map[string]struct{}{},
		inflight: map[int64]context.CancelFunc{},
	}}
	s.root = s
	if err := peer.Handle(InvokeMethod, s.onInvoke); err != nil {
		return nil, err
	}
	if err := peer.HandleEvent(ReleaseEvent, s.onRelease); err != nil {
		return nil, err
	}
	scopes.Store(peer, s)
	go s.watch()
	return s, nil
}

// ScopeOf is the scope over a peer, which is how generated code inside a
// handler reaches the layer: the handler has the peer, and the peer has one
// scope or none.
func ScopeOf(peer *runtime.Peer) (*Scope, bool) {
	if peer == nil {
		return nil, false
	}
	s, ok := scopes.Load(peer)
	if !ok {
		return nil, false
	}
	return s.(*Scope), true
}

// Peer is the peer the scope runs over.
func (s *Scope) Peer() *runtime.Peer { return s.peer }

// watch ends the scope with the connection carrying it. A reference does not
// survive its connection and reconnection revives nothing: the next connection
// is another scope, whose nonce is another nonce.
func (s *Scope) watch() {
	<-s.peer.Done()
	scopes.Delete(s.peer)
	s.end()
}

// Close ends the scope without ending the peer: every binding is invalidated
// and every invocation this side has in flight is settled. It is not a
// barrier — a barrier is what Release is — and it does not roll back an effect
// an invocation already had.
func (s *Scope) Close() error {
	scopes.Delete(s.peer)
	s.end()
	return nil
}

func (s *Scope) end() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	cancels := make([]context.CancelFunc, 0, len(s.inflight))
	for _, cancel := range s.inflight {
		cancels = append(cancels, cancel)
	}
	s.inflight = map[int64]context.CancelFunc{}
	s.exports = map[string]*binding{}
	s.imports = map[string]*attachment{}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// Counts is what the scope holds now.
func (s *Scope) Counts() Counts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Counts{Exports: len(s.exports), Imports: len(s.imports)}
}

// Export makes a binding of a native function and returns the reference that
// names it. Exporting the same function twice makes two bindings: native
// identity is nobody's guarantee across a wire, and two bindings are two
// lifetimes, which is what separate release needs.
func (s *Scope) Export(contract string, invoke Invoke) (Reference, error) {
	if contract == "" {
		return Reference{}, s.refuse(contract, ErrorContractInvalid, "a binding is exported for a contract")
	}
	if invoke == nil {
		return Reference{}, s.refuse(contract, ErrorContractInvalid, "a binding is a function")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Reference{}, s.refuse(contract, ErrorScopeClosed, "the scope ended")
	}
	if len(s.exports) >= s.options.MaxExports {
		s.mu.Unlock()
		return Reference{}, s.refuse(contract, ErrorTooManyExports, "no room for another exported binding")
	}
	id := s.nonce + "." + strconv.FormatInt(s.next, 10)
	s.next++
	s.exports[id] = &binding{contract: contract, invoke: invoke}
	if s.batch != nil && s.batch.active {
		s.batch.ids = append(s.batch.ids, id)
	}
	s.mu.Unlock()
	s.observeExported(contract, id)
	return Reference{binding: id, contract: contract, scope: s.root}, nil
}

// Decode validates serialized binding and contract strings and associates the
// resulting reference with this scope. It accepts caller-supplied bytes without
// proving they arrived in a message, name an existing binding, or authorize its
// use. Import checks the native association; invocation checks the export table.
func (s *Scope) Decode(raw json.RawMessage) (Reference, error) {
	var wire referenceWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Reference{}, &runtime.PublicError{Code: ErrorContractInvalid, Message: "a live reference is a binding and a contract"}
	}
	if wire.Binding == "" || wire.Contract == "" {
		return Reference{}, &runtime.PublicError{Code: ErrorContractInvalid, Message: "a live reference is a binding and a contract"}
	}
	return Reference{binding: wire.Binding, contract: wire.Contract, scope: s.root}, nil
}

// Import is the attachment to a binding, as a function to call it with. The
// same binding imported again gives the same attachment: one binding is one
// dispatch, and two imports never become two readers competing for one reply.
// A reference this side exported resolves to the function behind it, with
// nothing crossing the wire. Attaching to a remote binding does not establish
// that it exists: that lookup occurs when the imported function is invoked.
func (s *Scope) Import(r Reference, contract string) (Invoke, error) {
	if contract == "" {
		return nil, s.refuse(contract, ErrorContractInvalid, "a binding is imported for a contract")
	}
	if r.scope == nil || r.scope != s.root {
		return nil, s.refuse(contract, ErrorReferenceForeign, "the reference was minted in another scope")
	}
	if r.contract != contract {
		return nil, s.refuse(contract, ErrorContractMismatch, "the reference carries "+r.contract+" where "+contract+" is expected")
	}
	// The table is the scope's own and nobody is told while it is held: an
	// observer runs where the event happened, and this one would otherwise run
	// under the lock every invocation of the scope waits on.
	invoke, fresh, code, message := s.attach(r, contract)
	switch {
	case code != "":
		return nil, s.refuse(contract, code, message)
	case fresh:
		s.observeImported(contract, r.binding)
	}
	return invoke, nil
}

func (s *Scope) attach(r Reference, contract string) (invoke Invoke, fresh bool, code, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, false, ErrorScopeClosed, "the scope ended"
	}
	if _, tombstoned := s.gone[r.binding]; tombstoned {
		return nil, false, ErrorReferenceReleased, "the binding was released"
	}
	if own, ok := s.exports[r.binding]; ok {
		if own.contract != contract {
			return nil, false, ErrorContractMismatch, "the binding carries " + own.contract
		}
		return s.local(own), false, "", ""
	}
	if held, ok := s.imports[r.binding]; ok {
		if held.contract != contract {
			return nil, false, ErrorContractMismatch, "the binding is attached as " + held.contract
		}
		return held.invoke, false, "", ""
	}
	if len(s.imports) >= s.options.MaxImports {
		return nil, false, ErrorTooManyImports, "no room for another imported binding"
	}
	held := &attachment{contract: contract}
	held.invoke = s.remote(r.binding, held)
	s.imports[r.binding] = held
	return held.invoke, true, "", ""
}

// local is a reference of this side's own making, handed back and imported
// here. It reaches the function behind the binding without a frame: the
// alternative is a peer speaking to itself down its own connection.
func (s *Scope) local(own *binding) Invoke {
	return func(ctx context.Context, request json.RawMessage) (json.RawMessage, error) {
		s.mu.Lock()
		closed, released := s.closed, own.released
		invoke := own.invoke
		s.mu.Unlock()
		switch {
		case closed:
			return nil, &runtime.PublicError{Code: ErrorScopeClosed, Message: "the scope ended"}
		case released:
			return nil, &runtime.PublicError{Code: ErrorReferenceReleased, Message: "the binding was released"}
		}
		return s.invokeScoped(ctx, invoke, request)
	}
}

// remote is an attachment to the other side's binding. Release is a barrier
// here: it refuses the next invocation and leaves the ones already sent to
// settle, since releasing a reference says nothing about work already asked
// for.
func (s *Scope) remote(id string, held *attachment) Invoke {
	return func(ctx context.Context, request json.RawMessage) (json.RawMessage, error) {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return nil, &runtime.PublicError{Code: ErrorScopeClosed, Message: "the scope ended"}
		}
		if held.released {
			s.mu.Unlock()
			return nil, &runtime.PublicError{Code: ErrorReferenceReleased, Message: "the binding was released"}
		}
		call, cancel := context.WithCancel(ctx)
		ticket := s.call
		s.call++
		s.inflight[ticket] = cancel
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.inflight, ticket)
			s.mu.Unlock()
			cancel()
		}()

		var result json.RawMessage
		err := s.peer.Call(call, InvokeMethod, invokeParams{
			Binding:  id,
			Contract: held.contract,
			Request:  request,
		}, &result)
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed && ctx.Err() == nil {
				return nil, &runtime.PublicError{Code: ErrorScopeClosed, Message: "the scope ended"}
			}
			return nil, err
		}
		return result, nil
	}
}

type invokeParams struct {
	Binding  string          `json:"binding"`
	Contract string          `json:"contract"`
	Request  json.RawMessage `json:"request,omitempty"`
}

type releaseData struct {
	Binding string `json:"binding"`
}

// onInvoke answers the other side's invocation of a binding of ours. It proves
// the binding is one this scope exported and that the contract named is the one
// it was exported for, and then it is the function's business.
func (s *Scope) onInvoke(ctx context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
	var params invokeParams
	if err := json.Unmarshal(raw, &params); err != nil || params.Binding == "" || params.Contract == "" {
		return nil, &runtime.PublicError{Code: ErrorContractInvalid, Message: "live.invoke names a binding and a contract"}
	}
	s.mu.Lock()
	closed := s.closed
	own, ok := s.exports[params.Binding]
	var contract string
	var released bool
	var invoke Invoke
	if ok {
		contract, released, invoke = own.contract, own.released, own.invoke
	}
	s.mu.Unlock()
	switch {
	case closed:
		return nil, s.refuse(params.Contract, ErrorScopeClosed, "the scope ended")
	case !ok:
		return nil, s.refuse(params.Contract, ErrorReferenceUnknown, "no binding "+params.Binding+" in this scope")
	case contract != params.Contract:
		return nil, s.refuse(params.Contract, ErrorContractMismatch, "the binding carries "+contract)
	case released:
		return nil, s.refuse(contract, ErrorReferenceReleased, "the binding was released")
	}
	result, err := s.invokeScoped(ctx, invoke, params.Request)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return json.RawMessage("null"), nil
	}
	return result, nil
}

// onRelease takes the other side's word that it is done with a binding, or
// that one it exported is gone. It releases here and tells nobody: the side
// that released already knows.
func (s *Scope) onRelease(_ context.Context, _ *runtime.Peer, raw json.RawMessage) {
	var data releaseData
	if err := json.Unmarshal(raw, &data); err != nil || data.Binding == "" {
		return
	}
	s.release(data.Binding, false)
}

// Release invalidates a binding and every alias of it, idempotently, whether
// this side exported it or imported it, and tells the other side. It is a
// barrier: the next invocation is refused reference_released and the ones
// already dispatched settle and are delivered. It releases nothing else, and it
// is not a cancellation — neither of an invocation in flight nor of whatever
// the application does behind the callable.
func (s *Scope) Release(r Reference) error {
	if r.scope == nil || r.scope != s.root {
		return s.refuse(r.contract, ErrorReferenceForeign, "the reference was minted in another scope")
	}
	s.release(r.binding, true)
	return nil
}

func (s *Scope) release(id string, tell bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	contract := ""
	found := false
	if own, ok := s.exports[id]; ok {
		own.released = true
		contract, found = own.contract, true
		delete(s.exports, id)
	}
	if held, ok := s.imports[id]; ok {
		held.released = true
		contract, found = held.contract, true
		delete(s.imports, id)
	}
	_, tombstoned := s.gone[id]
	if tombstoned && !found {
		s.mu.Unlock()
		return
	}
	// What a released binding leaves behind is one id, so that an alias
	// imported again is refused for the reason it was actually refused for.
	// The set is bounded by the tables it shadows and forgets its oldest past
	// that: a binding whose tombstone is gone is refused reference_unknown by
	// the side that exported it, which is a refusal either way.
	if !tombstoned {
		s.gone[id] = struct{}{}
		s.order = append(s.order, id)
		for len(s.order) > s.options.MaxExports+s.options.MaxImports {
			delete(s.gone, s.order[0])
			s.order = s.order[1:]
		}
	}
	s.mu.Unlock()
	if found {
		s.observeReleased(contract, id)
	}
	if tell {
		_ = s.peer.Emit(s.peer.Context(), ReleaseEvent, releaseData{Binding: id})
	}
}

// Forward gives another scope a binding of its own over a callable this one
// imported. It is composition, not a mechanism: the destination gets an
// ordinary export whose function is the origin's, and so a lifetime of its own.
// Releasing the forwarded binding does not release the origin; an invocation
// through a released or ended origin fails with the origin's refusal, which is
// what the destination's caller is told.
func Forward(destination *Scope, contract string, origin Invoke) (Reference, error) {
	if destination == nil {
		return Reference{}, errors.New("forwarding needs a destination scope")
	}
	if origin == nil {
		return Reference{}, &runtime.PublicError{Code: ErrorContractInvalid, Message: "forwarding needs the origin's function"}
	}
	return destination.Export(contract, origin)
}

func (s *Scope) refuse(contract, code, message string) error {
	s.observeRefused(contract, code, message)
	return &runtime.PublicError{Code: code, Message: message}
}
