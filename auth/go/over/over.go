// Package over is the profile over a peer — the adapter #356 owes on the
// packets, the one package of the module that composes onto the runtime:
// the auth.challenge / auth.prove exchange installed beside live's handlers,
// the one context the connection then has, reachable from any handler of
// the connection through the context it runs with; the guard an
// implementation calls before a handler's body and again at the owner's
// effect; and the surface of a generated side read off its declaration.
// The runtime knows nothing of it: the exchange is two named handlers, the
// context lives with the layer, and a guard is a call the implementation
// makes. The pure packages beside it — auth and grant — read no clock and
// take no context; this one takes the handler's.
package over

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/Bitspark/nightseam/auth/go"
	"github.com/Bitspark/nightseam/auth/go/grant"
	"github.com/Bitspark/nightseam/runtime/go"
)

// ChallengeMethod and ProveMethod are the exchange's two requests, under
// the reserved prefix the built-in family auth declares.
const (
	ChallengeMethod = "auth.challenge"
	ProveMethod     = "auth.prove"
)

// Options is what the layer over a peer is given: the root every decision
// is held to, the audience the connection reached as the service knows
// itself — empty for a presentation with no URL — the decision time, read
// at each decision and never by the layer on its own, and the server's
// entropy, one nonce per challenge.
type Options struct {
	Root     grant.Root
	Audience string
	Now      func() grant.Time
	Nonce    func() [32]byte
}

// slot is where a connection's context lives: placed on the connection's
// context by Prepare or Trust before the peer is built, found there by Over,
// read there by ContextOf. A channel tunnelled over the connection inherits
// the context and with it the slot.
type slot struct {
	mu      sync.Mutex
	layer   *Layer
	trusted *auth.Context
}

type slotKey struct{}

func slotOf(ctx context.Context) *slot {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(slotKey{}).(*slot)
	return s
}

// Prepare returns ctx carrying the place a connection's context will live
// once the exchange has made one. A server returns it from Authenticate; a
// pipe's constructor builds its peer with it.
func Prepare(ctx context.Context) context.Context {
	return context.WithValue(ctx, slotKey{}, &slot{})
}

// Trust returns ctx carrying a context given by construction — trust, not
// authentication — for a peer that runs no exchange: a pipe, a model wire
// with no peer between. The layer over such a peer refuses the exchange as
// auth.Established.
func Trust(ctx context.Context, trusted *auth.Context) context.Context {
	return context.WithValue(ctx, slotKey{}, &slot{trusted: trusted})
}

// ContextOf is the connection's context from a handler's ctx — a method's,
// an event's, an exported callable's, which runs under the context of the
// connection that invokes it — or nil before the exchange has made one.
func ContextOf(ctx context.Context) *auth.Context {
	s := slotOf(ctx)
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trusted != nil {
		return s.trusted
	}
	if s.layer == nil {
		return nil
	}
	return s.layer.connection.Context()
}

// Layer is the profile over one peer: its exchange and its options.
type Layer struct {
	peer       *runtime.Peer
	options    Options
	connection auth.Connection
	slot       *slot
}

// Over installs the exchange on a peer and binds the layer to the place
// the peer's context was prepared with; a peer whose context carries none
// is refused, since no handler of it could ever find a context.
func Over(peer *runtime.Peer, options Options) (*Layer, error) {
	if peer == nil {
		return nil, errors.New("the profile is composed onto a peer")
	}
	if options.Now == nil || options.Nonce == nil {
		return nil, errors.New("the profile needs the decision time and the entropy from its caller")
	}
	s := slotOf(peer.Context())
	if s == nil {
		return nil, errors.New("the peer's context was not prepared for the profile: return auth.Prepare(ctx) from Authenticate, or build the peer with it")
	}
	connection, err := auth.NewConnection(options.Audience)
	if err != nil {
		return nil, err
	}
	l := &Layer{peer: peer, options: options, connection: connection, slot: s}
	s.mu.Lock()
	if s.layer != nil {
		s.mu.Unlock()
		return nil, errors.New("the peer already carries the profile")
	}
	s.layer = l
	s.mu.Unlock()
	if err := peer.Handle(ChallengeMethod, l.challenge); err != nil {
		return nil, err
	}
	if err := peer.Handle(ProveMethod, l.prove); err != nil {
		return nil, err
	}
	return l, nil
}

// auth.Context is the connection's one context: established, or given by
// construction; nil before either.
func (l *Layer) Context() *auth.Context {
	l.slot.mu.Lock()
	trusted := l.slot.trusted
	l.slot.mu.Unlock()
	if trusted != nil {
		return trusted
	}
	return l.connection.Context()
}

func (l *Layer) trusted() bool {
	l.slot.mu.Lock()
	defer l.slot.mu.Unlock()
	return l.slot.trusted != nil
}

func (l *Layer) challenge(context.Context, *runtime.Peer, json.RawMessage) (any, error) {
	if l.trusted() {
		return nil, (&auth.Refusal{Code: auth.Established}).Public()
	}
	nonce := l.options.Nonce()
	kept, refused := l.connection.Challenge(nonce[:])
	if refused != nil {
		return nil, refused.Public()
	}
	return map[string]string{"nonce": hex.EncodeToString(kept)}, nil
}

func (l *Layer) prove(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
	if l.trusted() {
		return nil, (&auth.Refusal{Code: auth.Established}).Public()
	}
	subject, possession, chain, ok := parseProve(raw)
	if !ok {
		return nil, (&auth.Refusal{Code: auth.Malformed}).Public()
	}
	ctx, refused := l.connection.Prove(l.options.Root, subject, possession, chain, l.options.Now())
	if refused != nil {
		return nil, refused.Public()
	}
	if ctx.Validity.Finite {
		return map[string]uint64{"expires_at": ctx.Validity.ExpiresAt}, nil
	}
	return map[string]any{}, nil
}

// parseProve reads the prove request as the built-in family declares it:
// the subject as ed25519: and 64 hex characters, the possession as 128,
// and the chain as a list of hex envelopes.
func parseProve(raw json.RawMessage) (subject [32]byte, possession []byte, chain [][]byte, ok bool) {
	var params struct {
		Subject    string   `json:"subject"`
		Possession string   `json:"possession"`
		Chain      []string `json:"chain"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return subject, nil, nil, false
	}
	key, found := strings.CutPrefix(params.Subject, "ed25519:")
	if !found {
		return subject, nil, nil, false
	}
	keyBytes, err := hex.DecodeString(key)
	if err != nil || len(keyBytes) != 32 {
		return subject, nil, nil, false
	}
	copy(subject[:], keyBytes)
	if possession, err = hex.DecodeString(params.Possession); err != nil || len(possession) != 64 {
		return subject, nil, nil, false
	}
	if params.Chain == nil {
		return subject, nil, nil, false
	}
	for _, entry := range params.Chain {
		envelope, err := hex.DecodeString(entry)
		if err != nil || len(envelope) == 0 {
			return subject, nil, nil, false
		}
		chain = append(chain, envelope)
	}
	return subject, possession, chain, true
}

// ---- the guard ----------------------------------------------------------------------

// Guard is what an implementation of an exposed side calls: Decide at the
// top of a handler, Effect inside its transaction, Invoke from an exported
// callable, Emit before an event leaves. Each refusal is the profile's
// public error and crosses the wire with its code.
type Guard struct {
	Binding *auth.Binding
	Root    grant.Root
	Now     func() grant.Time
}

// NewGuard is a guard over a bound policy; construction refuses as Bind
// does, so a guard exists only for a policy that binds whole.
func NewGuard(binding *auth.Binding, root grant.Root, now func() grant.Time) (*Guard, error) {
	if binding == nil || now == nil {
		return nil, errors.New("a guard is over a bound policy and reads the decision time from its caller")
	}
	return &Guard{Binding: binding, Root: root, Now: now}, nil
}

// Decide is the decision at dispatch, on the context the handler runs with.
func (g *Guard) Decide(ctx context.Context, member string, payload map[string]any) (*auth.Decision, error) {
	decision, refused := g.Binding.Decide(g.Root, member, payload, "", ContextOf(ctx), g.Now())
	if refused != nil {
		return nil, refused.Public()
	}
	return decision, nil
}

// Effect is the decision at the effect: the same decision at the effect's
// own time, then the owner's condition over the resolved target.
func (g *Guard) Effect(ctx context.Context, decision *auth.Decision, condition auth.Condition) error {
	if refused := g.Binding.Effect(g.Root, decision, ContextOf(ctx), g.Now(), condition); refused != nil {
		return refused.Public()
	}
	return nil
}

// Export records what a returned callable is to this exposure.
func (g *Guard) Export(ref, member, scope string) error { return g.Binding.Export(ref, member, scope) }

// Invoke is Decide for a reference this exposure exported, under the
// invoking connection's context.
func (g *Guard) Invoke(ctx context.Context, ref string, payload map[string]any) (*auth.Decision, error) {
	decision, refused := g.Binding.Invoke(g.Root, ref, payload, ContextOf(ctx), g.Now())
	if refused != nil {
		return nil, refused.Public()
	}
	return decision, nil
}

// Emit decides an event per recipient at emission time: each recipient is
// named with the context its connection runs handlers with.
func (g *Guard) Emit(event string, data map[string]any, to []auth.Recipient) []auth.Delivery {
	return g.Binding.Emit(g.Root, event, data, to, g.Now())
}

// RecipientOf names a recipient by the context its connection runs with.
func RecipientOf(name string, ctx context.Context) auth.Recipient {
	return auth.Recipient{Name: name, Ctx: ContextOf(ctx)}
}

// Payload is a typed request's top-level members as the wire spells them,
// which is what a scope template's holes are filled from.
func Payload(params any) map[string]any {
	data, err := json.Marshal(params)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(data, &out) != nil || out == nil {
		return map[string]any{}
	}
	return out
}

// ---- the surface ---------------------------------------------------------------------

// SurfaceOf reads a side's members and the family's callables off the
// declaration the generator emitted — protocol.WireDeclaration() and
// protocol.WireDigest() — with the top-level names of each request payload.
// Nothing is written by hand, so a member the declaration gains fails Bind
// until the policy names it.
func SurfaceOf(family, side, declaration, digest string) (auth.Surface, error) {
	var decl struct {
		Definitions map[string]json.RawMessage `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(declaration), &decl); err != nil {
		return auth.Surface{}, err
	}
	def := func(ref string, into any) error {
		raw, ok := decl.Definitions[ref]
		if !ok {
			return errors.New("the declaration has no " + ref)
		}
		return json.Unmarshal(raw, into)
	}
	var fam struct {
		Server, Client struct{ Ref string }
		Types          map[string]struct{ Ref string }
	}
	if err := def(family, &fam); err != nil {
		return auth.Surface{}, err
	}
	ref := fam.Server.Ref
	if side == "client" {
		ref = fam.Client.Ref
	} else if side != "server" {
		return auth.Surface{}, errors.New("a side is server or client")
	}
	var sd struct {
		Methods map[string]struct{ Request json.RawMessage }
		Events  map[string]json.RawMessage
	}
	if ref != "" {
		if err := def(ref, &sd); err != nil {
			return auth.Surface{}, err
		}
	}
	fieldsOf := func(expr json.RawMessage) []string {
		var e struct{ Ref string }
		if len(expr) == 0 || json.Unmarshal(expr, &e) != nil || e.Ref == "" {
			return nil
		}
		var rec struct {
			Kind   string
			Fields []struct{ Name string }
		}
		if def(e.Ref, &rec) != nil || rec.Kind != "record" {
			return nil
		}
		names := make([]string, 0, len(rec.Fields))
		for _, f := range rec.Fields {
			names = append(names, f.Name)
		}
		sort.Strings(names)
		return names
	}
	s := auth.Surface{Family: family, Digest: digest}
	for name, m := range sd.Methods {
		s.Members = append(s.Members, auth.Member{Key: "method:" + name, Fields: fieldsOf(m.Request)})
	}
	for name, data := range sd.Events {
		s.Members = append(s.Members, auth.Member{Key: "event:" + name, Fields: fieldsOf(data)})
	}
	for name, t := range fam.Types {
		var typ struct {
			Kind    string
			Request json.RawMessage
		}
		if err := def(t.Ref, &typ); err == nil && typ.Kind == "callable" {
			s.Members = append(s.Members, auth.Member{Key: "callable:" + name, Fields: fieldsOf(typ.Request)})
		}
	}
	sort.Slice(s.Members, func(i, j int) bool { return s.Members[i].Key < s.Members[j].Key })
	return s, nil
}
