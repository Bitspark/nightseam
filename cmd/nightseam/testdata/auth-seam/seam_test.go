// AUTH-SEAM: an ordinary consumer guards the generated access surface with a
// deterministic synthetic caller context — no cryptography, no private
// tables, no auth in the base wire — over every presentation the tree
// offers. The exposure is held to docs/auth/exposure.md: a treatment per
// member derived from the generated declaration, bound whole or refused,
// decided at dispatch and again at the owner's effect, on the context the
// runtime delivers beside the frame. What a real profile adds (#353, #354,
// #356) is the evidence, never the placement.
package seam_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	bitwire "github.com/Bitspark/bitwire/wire/go"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cellbinding "example.test/generated/api/go/cell-binding"
	cell "example.test/generated/api/go/cell-protocol"
	holderbinding "example.test/generated/api/go/holder-binding"
	holder "example.test/generated/api/go/holder-protocol"
	ops "example.test/generated/api/go/ops-protocol"
	binding "example.test/generated/api/go/worker-binding"
	protocol "example.test/generated/api/go/worker-protocol"
	binding2 "example.test/generated/api/go/worker2-binding"
	protocol2 "example.test/generated/api/go/worker2-protocol"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/tunnel/go"
)

// ---- the synthetic authority ---------------------------------------------------

// grant stands where a verified chain would: what a subject may do, where,
// until when. Coverage is the grant table's rule — a scope covers itself
// and everything under scope + "/".
type grant struct {
	actions []string
	scope   string
	expires uint64 // 0: unbounded
}

var authority = map[string][]grant{
	"alice":  {{[]string{"read", "write"}, "projects", 0}},
	"bob":    {{[]string{"read", "write"}, "projects/7", 2000}},
	"carol":  {{[]string{"read"}, "projects/7/ledger", 2000}},
	"server": {{[]string{"read"}, "projects/7", 0}},
}

// gate is one connection's context: nothing until established, then one
// subject, immutable for the connection's life.
type gate struct {
	mu          sync.Mutex
	subject     string
	established bool
}

func (g *gate) who() (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.subject, g.established
}

func trusted(subject string) *gate { return &gate{subject: subject, established: true} }

type gateKey struct{}

func gateOf(ctx context.Context) *gate { g, _ := ctx.Value(gateKey{}).(*gate); return g }

func withGate(ctx context.Context, g *gate) context.Context {
	return context.WithValue(ctx, gateKey{}, g)
}

// fixedGate is a constructor's context for a route with no connection: the
// adapter's propagator places it on every dispatch, as Authenticate places
// one on a socket's and NewPeer on a pipe's.
type fixedGate struct{ g *gate }

func (f fixedGate) Extract(ctx context.Context, trace runtime.Trace) context.Context {
	return withGate(runtime.DefaultPropagator.Extract(ctx, trace), f.g)
}

func (fixedGate) Inject(ctx context.Context) runtime.Trace {
	return runtime.DefaultPropagator.Inject(ctx)
}

// clock is the decision time, advanced deliberately by a test.
var clock atomic.Uint64

func covered(g grant, action, scope string) bool {
	has := false
	for _, a := range g.actions {
		has = has || a == action
	}
	return has && (g.scope == scope || strings.HasPrefix(scope, g.scope+"/"))
}

func public(code, message string) error { return &runtime.PublicError{Code: code, Message: message} }

// call is connection.md's Call in miniature: the context's subject held to
// the request at this time. A refusal is a code, never prose about a grant.
func call(ctx context.Context, action, scope string) error {
	g := gateOf(ctx)
	if g == nil {
		return public("auth.unauthenticated", "no context on this connection")
	}
	subject, established := g.who()
	if !established {
		return public("auth.unauthenticated", "no context established")
	}
	now := clock.Load()
	for _, gr := range authority[subject] {
		if gr.expires != 0 && now >= gr.expires {
			continue
		}
		if covered(gr, action, scope) {
			return nil
		}
	}
	return public("auth.denied", action+" on "+scope)
}

// ---- the surface, from the generated declaration --------------------------------

type member struct {
	key    string
	fields []string
}

type surface struct {
	family, digest string
	members        map[string]member
}

// surfaceOf reads one side's members and the family's callables from the
// declaration the generator emitted — never from a hand-written list — so
// a member the declaration gains is a member the policy must name.
func surfaceOf(family, side, declaration, digest string) (surface, error) {
	var decl struct {
		Definitions map[string]json.RawMessage `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(declaration), &decl); err != nil {
		return surface{}, err
	}
	def := func(ref string, into any) error {
		raw, ok := decl.Definitions[ref]
		if !ok {
			return fmt.Errorf("declaration has no %q", ref)
		}
		return json.Unmarshal(raw, into)
	}
	var fam struct {
		Server, Client struct{ Ref string }
		Types          map[string]struct{ Ref string }
	}
	if err := def(family, &fam); err != nil {
		return surface{}, err
	}
	ref := fam.Server.Ref
	if side == "client" {
		ref = fam.Client.Ref
	}
	var sd struct {
		Methods map[string]struct{ Request json.RawMessage }
		Events  map[string]json.RawMessage
	}
	if err := def(ref, &sd); err != nil {
		return surface{}, err
	}
	fieldsOf := func(expr json.RawMessage) []string {
		var e struct{ Ref string }
		if json.Unmarshal(expr, &e) != nil || e.Ref == "" {
			return nil
		}
		var rec struct {
			Kind   string
			Fields []struct{ Name string }
		}
		if def(e.Ref, &rec) != nil || rec.Kind != "record" {
			return nil
		}
		var names []string
		for _, f := range rec.Fields {
			names = append(names, f.Name)
		}
		sort.Strings(names)
		return names
	}
	s := surface{family: family, digest: digest, members: map[string]member{}}
	for name, m := range sd.Methods {
		s.members["method:"+name] = member{"method:" + name, fieldsOf(m.Request)}
	}
	for name, data := range sd.Events {
		s.members["event:"+name] = member{"event:" + name, fieldsOf(data)}
	}
	for name, t := range fam.Types {
		var typ struct {
			Kind    string
			Request json.RawMessage
		}
		if err := def(t.Ref, &typ); err == nil && typ.Kind == "callable" {
			s.members["callable:"+name] = member{"callable:" + name, fieldsOf(typ.Request)}
		}
	}
	return s, nil
}

// ---- the policy and its binding ----------------------------------------------------

type kind string

const (
	guarded kind = "guarded"
	open    kind = "public"
	denied  kind = "denied"
)

type treatment struct {
	kind          kind
	action, scope string
}

type policy struct {
	family, digest string
	treatments     map[string]treatment
}

type constructionError struct {
	code    string
	members []string
}

func (e *constructionError) Error() string { return e.code + ": " + strings.Join(e.members, ",") }

var hole = regexp.MustCompile(`\{([^{}]*)\}`)

// exposure is a constructed binding: every member has exactly one treatment.
type exposure struct {
	surface surface
	policy  policy
	mu      sync.Mutex
	exports map[string]string // reference → the scope the owner recorded
}

// bind holds the policy to the surface whole and constructs nothing on refusal.
func bind(s surface, p policy) (*exposure, error) {
	if p.family != s.family || p.digest != s.digest {
		return nil, &constructionError{code: "auth.contract_mismatch"}
	}
	var unbound, undeclared, invalid []string
	for key, m := range s.members {
		t, ok := p.treatments[key]
		if !ok {
			unbound = append(unbound, key)
			continue
		}
		switch t.kind {
		case guarded:
			if t.action == "" || t.scope == "" {
				invalid = append(invalid, key)
				continue
			}
			for _, h := range hole.FindAllStringSubmatch(t.scope, -1) {
				if h[1] == "export" {
					if !strings.HasPrefix(key, "callable:") {
						invalid = append(invalid, key)
					}
					continue
				}
				if i := sort.SearchStrings(m.fields, h[1]); i >= len(m.fields) || m.fields[i] != h[1] {
					invalid = append(invalid, key)
				}
			}
		default:
			if t.action != "" || t.scope != "" {
				invalid = append(invalid, key)
			}
		}
	}
	for key := range p.treatments {
		if _, ok := s.members[key]; !ok {
			undeclared = append(undeclared, key)
		}
	}
	for _, list := range []*[]string{&unbound, &undeclared, &invalid} {
		sort.Strings(*list)
	}
	switch {
	case len(unbound) > 0:
		return nil, &constructionError{"auth.member_unbound", unbound}
	case len(undeclared) > 0:
		return nil, &constructionError{"auth.member_undeclared", undeclared}
	case len(invalid) > 0:
		return nil, &constructionError{"auth.template_invalid", invalid}
	}
	return &exposure{surface: s, policy: p, exports: map[string]string{}}, nil
}

// render fills a template from the request — a selector, never authority.
func render(template string, payload map[string]any, export string) (string, error) {
	var err error
	out := hole.ReplaceAllStringFunc(template, func(h string) string {
		name := h[1 : len(h)-1]
		if name == "export" {
			return export
		}
		v, ok := payload[name]
		if !ok {
			err = errors.New("missing " + name)
			return ""
		}
		var s string
		switch x := v.(type) {
		case string:
			s = x
		case int64:
			s = strconv.FormatInt(x, 10)
		default:
			err = errors.New("not a scalar")
			return ""
		}
		if s == "" || strings.Contains(s, "/") {
			err = errors.New("selector")
			return ""
		}
		return s
	})
	return out, err
}

// decide is the decision at dispatch, and — called again at the owner's
// boundary with the effect's own time — the decision at the effect.
func (e *exposure) decide(ctx context.Context, key string, payload map[string]any, export string) error {
	if _, ok := e.surface.members[key]; !ok {
		return public("auth.unknown_member", key)
	}
	t := e.policy.treatments[key]
	switch t.kind {
	case denied:
		return public("auth.member_denied", key)
	case open:
		return nil
	}
	scope, err := render(t.scope, payload, export)
	if err != nil {
		return public("auth.selector_invalid", err.Error())
	}
	return call(ctx, t.action, scope)
}

// export records what a callable reference is to this exposure; invoke is
// decide for that record, under the invoking connection's context.
func (e *exposure) export(ref, scope string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exports[ref] = scope
}

func (e *exposure) invoke(ctx context.Context, ref, key string) error {
	e.mu.Lock()
	scope, ok := e.exports[ref]
	e.mu.Unlock()
	if !ok {
		return public("auth.reference_unknown", ref)
	}
	return e.decide(ctx, key, nil, scope)
}

// ---- the worker exposure -----------------------------------------------------------

func workerSurface(t *testing.T) surface {
	t.Helper()
	s, err := surfaceOf("worker", "server", protocol.WireDeclaration(), protocol.WireDigest())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func workerPolicy() policy {
	return policy{family: "worker", digest: protocol.WireDigest(), treatments: map[string]treatment{
		"method:list":           {guarded, "read", "projects/{projectId}"},
		"method:whoami":         {open, "", ""},
		"method:start":          {guarded, "write", "projects/{projectId}"},
		"method:subscribe":      {guarded, "read", "projects/{projectId}"},
		"event:progress":        {guarded, "read", "projects/{projectId}"},
		"callable:JobStatus":    {guarded, "read", "{export}"},
		"callable:JobCancel":    {guarded, "write", "{export}"},
		"callable:ProgressSink": {denied, "", ""},
	}}
}

// counters observe the forbidden effects: a protected handler body that
// ran, an effect that applied, an emission that left, a callable that ran.
type counters struct {
	dispatched, effects, emissions, callables atomic.Int64
}

// worker is the owner. Its methods are what the generated binding dispatches
// into; each decides at entry on the context the binding delivers, and again
// at its effect, inside its own lock — the owner's transaction — where the
// project's state is read. hook runs between the two, so a test can change
// the world after the early check.
type worker struct {
	exp      *exposure
	c        *counters
	client   protocol.Client // this connection's client access: where events go
	connGate *gate           // this connection's context, for emission
	state    *projects
	hook     func()
}

type projects struct {
	mu       sync.Mutex
	archived map[string]bool
}

func (p *projects) archive(id string) { p.mu.Lock(); defer p.mu.Unlock(); p.archived[id] = true }

func (w *worker) List(ctx context.Context, params protocol.ListRequest) (protocol.Listing, error) {
	payload := map[string]any{"projectId": params.ProjectId}
	if err := w.exp.decide(ctx, "method:list", payload, ""); err != nil {
		return protocol.Listing{}, err
	}
	w.c.dispatched.Add(1)
	return protocol.Listing{Count: 1}, nil
}

func (w *worker) Whoami(ctx context.Context) (string, error) {
	if err := w.exp.decide(ctx, "method:whoami", nil, ""); err != nil {
		return "", err
	}
	if g := gateOf(ctx); g != nil {
		if subject, ok := g.who(); ok {
			return subject, nil
		}
	}
	return "", nil
}

func (w *worker) Start(ctx context.Context, params protocol.StartRequest) (protocol.Job, error) {
	payload := map[string]any{"projectId": params.ProjectId}
	if err := w.exp.decide(ctx, "method:start", payload, ""); err != nil {
		return protocol.Job{}, err
	}
	w.c.dispatched.Add(1)
	if w.hook != nil {
		w.hook()
	}
	// The effect: the same decision at the effect's time, then the owner's
	// own condition over the resolved target, both under the owner's lock.
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	if err := w.exp.decide(ctx, "method:start", payload, ""); err != nil {
		return protocol.Job{}, err
	}
	if w.state.archived[params.ProjectId] {
		return protocol.Job{}, public(protocol.ErrorArchived, params.ProjectId)
	}
	w.c.effects.Add(1)
	export := "projects/" + params.ProjectId
	ref := "job:" + params.ProjectId + ":" + strconv.FormatInt(w.c.effects.Load(), 10)
	w.exp.export(ref, export)
	// The returned callables are the export record's closures: whoever
	// invokes them, on whatever route, is decided against the record at
	// that time, under the invoking connection's context.
	return protocol.Job{
		ProjectId: params.ProjectId,
		Status: func(ctx context.Context, id string) (string, error) {
			if err := w.exp.invoke(ctx, ref, "callable:JobStatus"); err != nil {
				return "", err
			}
			w.c.callables.Add(1)
			return "running:" + id, nil
		},
		Cancel: func(ctx context.Context, id string) (bool, error) {
			if err := w.exp.invoke(ctx, ref, "callable:JobCancel"); err != nil {
				return false, err
			}
			w.c.callables.Add(1)
			return true, nil
		},
	}, nil
}

func (w *worker) Subscribe(ctx context.Context, params protocol.Subscription) (bool, error) {
	if err := w.exp.decide(ctx, "method:subscribe", map[string]any{"projectId": params.ProjectId}, ""); err != nil {
		return false, err
	}
	w.c.dispatched.Add(1)
	// The supplied sink is the subscriber's exposure: reporting into it is a
	// call the subscriber's own guard decides, under this side's context.
	return params.Sink(ctx, protocol.Progress{ProjectId: params.ProjectId, Percent: 1})
}

// emit is a disclosure toward this connection, decided against its context
// at emission time; refused, the event never leaves.
func (w *worker) emit(data protocol.Progress) error {
	ctx := withGate(context.Background(), w.connGate)
	if err := w.exp.decide(ctx, "event:progress", map[string]any{"projectId": data.ProjectId}, ""); err != nil {
		return err
	}
	w.c.emissions.Add(1)
	return w.client.Events.Progress(ctx, data)
}

// ---- the synthetic exchange ------------------------------------------------------------

// installProve installs auth.prove: one establishment per connection, a
// second refused. A raw wire handler under the auth prefix on the
// connection's dispatcher, as the real exchange will be; the base wire
// knows nothing of it.
func installProve(registry runtime.HandlerRegistry) error {
	_, err := runtime.HandleWire(registry, []string{"auth", "prove"}, func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			Subject string `json:"subject"`
		}
		if err := json.Unmarshal(raw, &params); err != nil || params.Subject == "" {
			return nil, public("auth.malformed", "subject")
		}
		g := gateOf(ctx)
		if g == nil {
			return nil, public("auth.unsupported", "no gate on this connection")
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.established {
			return nil, public("auth.established", "a context is immutable")
		}
		g.subject, g.established = params.Subject, true
		return map[string]any{"subject": params.Subject}, nil
	})
	return err
}

// ---- servers ------------------------------------------------------------------------------

type served struct {
	server   *httptest.Server
	workers  sync.Map // *gate → *worker
	counters counters
	state    *projects
	hook     func()
	prefix   []string
	exposure *exposure
	refused  atomic.Int64
	prepared chan *runtime.Peer
	peers    sync.Map // *gate → *runtime.Peer, the server side of each connection
	controls func(*runtime.Peer) error
}

// mount binds the guarded worker onto a peer: the exposure constructed
// first, the generated ToWire around the implementation, forwarded onto the
// endpoint — the peer's own, or a view of its dispatcher at the root or
// under a prefix the server chooses.
func (s *served) mount(peer *runtime.Peer, scope *live.Scope, g *gate, target bitwire.Endpoint) error {
	if s.exposure == nil {
		return errors.New("no exposure: the connection is refused, nothing partial is attached")
	}
	wire, err := binding.ToWire(func(client protocol.Client) (protocol.Server, error) {
		w := &worker{exp: s.exposure, c: &s.counters, client: client, connGate: g, state: s.state, hook: s.hook}
		s.workers.Store(g, w)
		return protocol.Server{Methods: w, Events: struct{}{}}, nil
	}, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)})
	if err != nil {
		return err
	}
	detach, err := runtime.ForwardWire(target, wire)
	if err != nil {
		return err
	}
	go func() { <-peer.Done(); detach(); _ = wire.Close(duplex.CodeNormal, "") }()
	return nil
}

func (s *served) prepare(peer *runtime.Peer) error {
	// One dispatcher owns the peer endpoint's attachment; the exchange and
	// the model are routes on it — exact before longest prefix.
	dispatcher, err := runtime.NewDispatcher(peer.Wire())
	if err != nil {
		return err
	}
	if err := installProve(dispatcher); err != nil {
		return err
	}
	scope, err := live.Over(peer, live.Options{MaxImports: 32, MaxExports: 64})
	if err != nil {
		return err
	}
	if err := s.mount(peer, scope, gateOf(peer.Context()), dispatcher.Select(s.prefix)); err != nil {
		s.refused.Add(1)
		return err
	}
	s.peers.Store(gateOf(peer.Context()), peer)
	if s.controls != nil {
		if err := s.controls(peer); err != nil {
			return err
		}
	}
	if s.prepared != nil {
		s.prepared <- peer
	}
	return nil
}

func serve(t *testing.T, p policy, configure ...func(*served)) *served {
	t.Helper()
	s := &served{state: &projects{archived: map[string]bool{}}}
	if exp, err := bind(workerSurface(t), p); err == nil {
		s.exposure = exp
	}
	for _, c := range configure {
		c(s)
	}
	handler, err := runtime.NewHandler(runtime.ServerOptions{
		Authenticate: func(r *http.Request) (context.Context, error) { return withGate(r.Context(), &gate{}), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
		Options:      runtime.Options{Prepare: s.prepare},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.server = httptest.NewServer(handler)
	t.Cleanup(s.server.Close)
	return s
}

func (s *served) url() string { return "ws" + strings.TrimPrefix(s.server.URL, "http") }

// emitAll emits one progress toward every connection; the count delivered
// is the count of connections whose context covers the project.
func (s *served) emitAll(data protocol.Progress) (delivered, dropped int) {
	s.workers.Range(func(_, value any) bool {
		if err := value.(*worker).emit(data); err != nil {
			dropped++
		} else {
			delivered++
		}
		return true
	})
	return
}

// ---- clients --------------------------------------------------------------------------------

// client is one connection's user: its peer, its scope, its typed access and
// what it received. Its own exposure guards what it supplies to the server.
type client struct {
	peer       *runtime.Peer
	dispatcher *runtime.Dispatcher // when the model is selected under a prefix
	prefix     []string
	scope      *live.Scope
	access     protocol.Server
	mu         sync.Mutex
	got        []protocol.Progress
	notify     atomic.Int64
}

// endpoint is a fresh receiving view of the connection at the model's
// prefix: the peer's own endpoint at the root, a selection of its
// dispatcher under a prefix.
func (c *client) endpoint() bitwire.Endpoint {
	if c.dispatcher != nil {
		return c.dispatcher.Select(c.prefix)
	}
	return c.peer.Wire()
}

func (c *client) Progress(_ context.Context, data protocol.Progress) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, data)
	return nil
}

func (c *client) Notify(ctx context.Context, data protocol.Progress) (bool, error) {
	// A reverse call: the server, an explicit caller, meets this side's
	// guard under this side's context — the server it dialed.
	if err := call(ctx, "read", "projects/"+data.ProjectId); err != nil {
		return false, err
	}
	c.notify.Add(1)
	return true, nil
}

func (c *client) received() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.got) }

func dialAt(t *testing.T, ctx context.Context, url string, prefix []string) *client {
	t.Helper()
	c := &client{}
	peer, _, err := runtime.Dial(withGate(ctx, trusted("server")), url, runtime.DialOptions{Options: runtime.Options{Prepare: func(p *runtime.Peer) error {
		scope, err := live.Over(p, live.Options{MaxImports: 32, MaxExports: 64})
		c.scope = scope
		return err
	}}})
	if err != nil {
		t.Fatal(err)
	}
	c.peer = peer
	t.Cleanup(func() { peer.Close() })
	if len(prefix) > 0 {
		c.prefix = prefix
		if c.dispatcher, err = runtime.NewDispatcher(peer.Wire()); err != nil {
			t.Fatal(err)
		}
	}
	model, err := binding.FromWire(ctx, c.endpoint(), runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(c.scope)})
	if err != nil {
		t.Fatal(err)
	}
	if c.access, err = model(protocol.Client{Methods: c, Events: c}); err != nil {
		t.Fatal(err)
	}
	return c
}

func dial(t *testing.T, ctx context.Context, url string) *client { return dialAt(t, ctx, url, nil) }

func (c *client) prove(ctx context.Context, subject string) error {
	var out map[string]any
	return runtime.CallWire(ctx, c.peer.Wire(), []string{"auth", "prove"}, map[string]string{"subject": subject}, &out)
}

func code(err error) string {
	var p *runtime.PublicError
	if errors.As(err, &p) {
		return p.Code
	}
	if err == nil {
		return ""
	}
	return "error:" + err.Error()
}

func expect(t *testing.T, what string, err error, want string) {
	t.Helper()
	if code(err) != want {
		t.Fatalf("%s: got %q (%v), want %q", what, code(err), err, want)
	}
}

func await(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not observed in time", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ---- AUTH-SEAM-001: no protected handler before the gate opens ---------------------

func TestAUTHSEAM001NoProtectedHandlerBeforeEstablishment(t *testing.T) {
	clock.Store(1000)
	s := serve(t, workerPolicy())
	ctx := testContext(t)
	c := dial(t, ctx, s.url())
	// The generated handlers and auth.prove are installed in Prepare, before
	// the peer reads: the very first request meets the guard and is refused,
	// neither lost nor admitted.
	_, err := c.access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7"})
	expect(t, "protected method before establishment", err, "auth.unauthenticated")
	_, err = c.access.Methods.Start(ctx, protocol.StartRequest{ProjectId: "7"})
	expect(t, "protected live method before establishment", err, "auth.unauthenticated")
	// A public member is admitted by policy, not by failed authentication,
	// and sees no subject.
	if who, err := c.access.Methods.Whoami(ctx); err != nil || who != "" {
		t.Fatalf("public member before establishment: %q %v", who, err)
	}
	if s.counters.dispatched.Load() != 0 || s.counters.effects.Load() != 0 {
		t.Fatalf("a protected handler ran before establishment")
	}
	if err := c.prove(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	expect(t, "a context is immutable", c.prove(ctx, "alice"), "auth.established")
	if _, err := c.access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7"}); err != nil {
		t.Fatalf("after establishment: %v", err)
	}
	if who, _ := c.access.Methods.Whoami(ctx); who != "bob" {
		t.Fatalf("subject after establishment: %q", who)
	}
	if s.counters.dispatched.Load() != 1 {
		t.Fatalf("handlers run: %d", s.counters.dispatched.Load())
	}
}

// ---- AUTH-SEAM-002: one guarded implementation, every presentation ---------------------

// samePolicy is what every presentation is held to: bob reads and writes
// projects/7 and nothing beside or above it, whatever route the call took.
func samePolicy(t *testing.T, ctx context.Context, access protocol.Server) {
	t.Helper()
	if _, err := access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7"}); err != nil {
		t.Fatalf("own project: %v", err)
	}
	_, err := access.Methods.List(ctx, protocol.ListRequest{ProjectId: "8"})
	expect(t, "sibling project", err, "auth.denied")
	_, err = access.Methods.List(ctx, protocol.ListRequest{ProjectId: "70"})
	expect(t, "a prefix is not a subtree", err, "auth.denied")
	_, err = access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7/ledger"})
	expect(t, "a separator in a selector", err, "auth.selector_invalid")
	_, err = access.Methods.List(ctx, protocol.ListRequest{ProjectId: "../7"})
	expect(t, "traversal in a selector", err, "auth.selector_invalid")
}

func TestAUTHSEAM002SamePolicyEveryPresentation(t *testing.T) {
	clock.Store(1000)
	ctx := testContext(t)

	t.Run("socket", func(t *testing.T) {
		s := serve(t, workerPolicy())
		c := dial(t, ctx, s.url())
		if err := c.prove(ctx, "bob"); err != nil {
			t.Fatal(err)
		}
		samePolicy(t, ctx, c.access)
		if s.counters.dispatched.Load() != 1 {
			t.Fatalf("denied calls produced effects: %d", s.counters.dispatched.Load())
		}
	})

	t.Run("nested selection under a server prefix", func(t *testing.T) {
		// The server presents the model under a prefix; the client selects
		// it. A prefix is routing: it neither adds nor removes authority.
		s := serve(t, workerPolicy(), func(s *served) { s.prefix = []string{"nested", "protected"} })
		c := dialAt(t, ctx, s.url(), []string{"nested", "protected"})
		if err := c.prove(ctx, "bob"); err != nil {
			t.Fatal(err)
		}
		samePolicy(t, ctx, c.access)
		// The root has no model: the same name outside the prefix is unknown.
		var out json.RawMessage
		err := runtime.CallWire(ctx, c.peer.Wire(), []string{"list"}, protocol.ListRequest{ProjectId: "7"}, &out)
		expect(t, "outside the prefix", err, "method_not_found")
		// A client-side mount over the selected endpoint changes nothing either.
		mounted := duplex.At(duplex.Mount(map[string]bitwire.Endpoint{"admin": c.endpoint()}), []string{"admin"})
		if err := runtime.CallWire(ctx, mounted, []string{"list"}, protocol.ListRequest{ProjectId: "7"}, &out); err != nil {
			t.Fatalf("mounted route: %v", err)
		}
		err = runtime.CallWire(ctx, mounted, []string{"list"}, protocol.ListRequest{ProjectId: "8"}, &out)
		expect(t, "a mount does not authorize a sibling", err, "auth.denied")
	})

	t.Run("local pair, trusted by construction", func(t *testing.T) {
		// A local pair has no audience and runs no exchange: its context is
		// what the constructor gives the peer — trust, not authentication.
		// The same exposure, the same implementation, no socket.
		s := &served{state: &projects{archived: map[string]bool{}}}
		s.exposure, _ = bind(workerSurface(t), workerPolicy())
		left, right := duplex.Pipe(1 << 20)
		var serverScope, clientScope *live.Scope
		server, err := runtime.NewPeer(withGate(ctx, trusted("bob")), right, runtime.ServerRole, runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
			serverScope, err = live.Over(p, live.Options{})
			if err != nil {
				return err
			}
			return s.mount(p, serverScope, gateOf(p.Context()), p.Wire())
		}})
		if err != nil {
			t.Fatal(err)
		}
		defer server.Close()
		local, err := runtime.NewPeer(ctx, left, runtime.ClientRole, runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
			clientScope, err = live.Over(p, live.Options{})
			return err
		}})
		if err != nil {
			t.Fatal(err)
		}
		defer local.Close()
		model, err := binding.FromWire(ctx, local.Wire(), runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(clientScope)})
		if err != nil {
			t.Fatal(err)
		}
		access, _ := model(protocol.Client{Methods: &client{}, Events: &client{}})
		samePolicy(t, ctx, access)
		// The model wire accessed directly, with no peer between: the
		// caller's own context does not cross even in-process, so without
		// a constructed context the guard refuses — the honest answer for a
		// route nobody constructed a context for. The adapter's propagator
		// is where a constructor supplies one.
		s2 := &served{state: &projects{archived: map[string]bool{}}, exposure: s.exposure}
		direct, err := binding.ToWire(func(client protocol.Client) (protocol.Server, error) {
			return protocol.Server{Methods: &worker{exp: s2.exposure, c: &s2.counters, client: client, state: s2.state}, Events: struct{}{}}, nil
		}, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(clientScope)})
		if err != nil {
			t.Fatal(err)
		}
		defer direct.Close(duplex.CodeNormal, "")
		bare, err := binding.FromWire(ctx, direct, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(clientScope)})
		if err != nil {
			t.Fatal(err)
		}
		bareAccess, _ := bare(protocol.Client{Methods: &client{}, Events: &client{}})
		_, err = bareAccess.Methods.List(withGate(ctx, trusted("bob")), protocol.ListRequest{ProjectId: "7"})
		expect(t, "direct access: the caller's context does not cross", err, "auth.unauthenticated")
		if s2.counters.dispatched.Load() != 0 {
			t.Fatal("an unconstructed route ran a protected handler")
		}
		constructed, err := binding.ToWire(func(client protocol.Client) (protocol.Server, error) {
			return protocol.Server{Methods: &worker{exp: s2.exposure, c: &s2.counters, client: client, state: s2.state}, Events: struct{}{}}, nil
		}, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(clientScope), Options: runtime.Options{Propagator: fixedGate{trusted("bob")}}})
		if err != nil {
			t.Fatal(err)
		}
		defer constructed.Close(duplex.CodeNormal, "")
		built, err := binding.FromWire(ctx, constructed, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(clientScope)})
		if err != nil {
			t.Fatal(err)
		}
		builtAccess, _ := built(protocol.Client{Methods: &client{}, Events: &client{}})
		samePolicy(t, ctx, builtAccess)
	})

	t.Run("tunnel channel inherits the connection's context", func(t *testing.T) {
		// The channel's peer is built from the outer peer's context: what
		// the connection established is what the channel is decided by, and
		// a channel confers nothing beyond it.
		accepted := make(chan error, 1)
		var inner *served
		s := serve(t, workerPolicy(), func(s *served) {
			inner = &served{state: s.state, exposure: s.exposure}
			s.prepared = make(chan *runtime.Peer, 4)
		})
		go func() {
			peer := <-s.prepared
			tn, err := tunnel.New(peer, tunnel.Options{})
			if err != nil {
				accepted <- err
				return
			}
			_, err = tn.Accept(ctx, runtime.Options{Prepare: func(cp *runtime.Peer) error {
				scope, err := live.Over(cp, live.Options{})
				if err != nil {
					return err
				}
				return inner.mount(cp, scope, gateOf(cp.Context()), cp.Wire())
			}})
			accepted <- err
		}()
		c := dial(t, ctx, s.url())
		if err := c.prove(ctx, "bob"); err != nil {
			t.Fatal(err)
		}
		ct, err := tunnel.New(c.peer, tunnel.Options{})
		if err != nil {
			t.Fatal(err)
		}
		var channelScope *live.Scope
		ch, err := ct.Open(ctx, "worker", protocol.WireDigest(), runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
			channelScope, err = live.Over(p, live.Options{})
			return err
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := <-accepted; err != nil {
			t.Fatal(err)
		}
		model, err := binding.FromWire(ctx, ch, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(channelScope)})
		if err != nil {
			t.Fatal(err)
		}
		access, _ := model(protocol.Client{Methods: &client{}, Events: &client{}})
		samePolicy(t, ctx, access)
		if inner.counters.dispatched.Load() != 1 {
			t.Fatalf("channel effects: %d", inner.counters.dispatched.Load())
		}
	})
}

// ---- AUTH-SEAM-003: callables keep their guard; contexts do not cross -----------------

func TestAUTHSEAM003CallablesKeepTheirGuard(t *testing.T) {
	clock.Store(1000)
	s := serve(t, workerPolicy())
	ctx := testContext(t)
	bob := dial(t, ctx, s.url())
	if err := bob.prove(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	owner := bob.scope.Owner().Child()
	defer owner.Release()
	octx := live.WithOwner(ctx, owner)
	job, err := bob.access.Methods.Start(octx, protocol.StartRequest{ProjectId: "7"})
	if err != nil {
		t.Fatal(err)
	}
	// Each returned callable is decided at each invocation, under its own
	// action, against the export record — not against the start.
	if status, err := job.Status(octx, "j1"); err != nil || status != "running:j1" {
		t.Fatalf("status: %q %v", status, err)
	}
	if ok, err := job.Cancel(octx, "j1"); err != nil || !ok {
		t.Fatalf("cancel: %v", err)
	}
	// Two contexts do not cross: carol's connection holds the same
	// declared type and cannot use bob's project; her own authority decides
	// her own calls, including a read that bob's would not have covered.
	carol := dial(t, ctx, s.url())
	if err := carol.prove(ctx, "carol"); err != nil {
		t.Fatal(err)
	}
	_, err = carol.access.Methods.Start(ctx, protocol.StartRequest{ProjectId: "7"})
	expect(t, "carol may not write projects/7", err, "auth.denied")
	_, err = carol.access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7"})
	expect(t, "carol may not read projects/7", err, "auth.denied")
	// A reference is decided for whoever holds it: the server, handed
	// bob's job by another route, invokes it under its own context.
	s.workers.Range(func(key, value any) bool {
		w := value.(*worker)
		if subject, _ := key.(*gate).who(); subject != "bob" {
			return true
		}
		held, err := w.Start(withGate(context.Background(), trusted("alice")), protocol.StartRequest{ProjectId: "7"})
		if err != nil {
			t.Errorf("a self-referenced start: %v", err)
			return false
		}
		_, err = held.Status(withGate(context.Background(), trusted("carol")), "x")
		expect(t, "a self-referenced callable under another context", err, "auth.denied")
		_, err = held.Status(context.Background(), "x")
		expect(t, "a self-referenced callable without a context", err, "auth.unauthenticated")
		if _, err := held.Status(withGate(context.Background(), trusted("alice")), "x"); err != nil {
			t.Errorf("a self-referenced callable under a covering context: %v", err)
		}
		return false
	})
	// A supplied callable keeps the supplier's guard: the sink is the
	// client's exposure, decided by the client under its own context.
	var reported atomic.Int64
	sink := func(ctx context.Context, p protocol.Progress) (bool, error) {
		if err := call(ctx, "read", "projects/"+p.ProjectId); err != nil {
			return false, err
		}
		reported.Add(1)
		return true, nil
	}
	sowner := bob.scope.Owner().Child()
	defer sowner.Release()
	if ok, err := bob.access.Methods.Subscribe(live.WithOwner(ctx, sowner), protocol.Subscription{ProjectId: "7", Sink: sink}); err != nil || !ok {
		t.Fatalf("subscribe: %v", err)
	}
	if reported.Load() != 1 {
		t.Fatalf("the sink ran %d times", reported.Load())
	}
	// A fresh connection inherits nothing from an old peer, not even the
	// same person's establishment: bob again is nobody until he proves.
	again := dial(t, ctx, s.url())
	_, err = again.access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7"})
	expect(t, "a reconnection is not established", err, "auth.unauthenticated")
	// The reverse direction is explicit: the server calls the client's
	// method, and the client's guard decides under the client's context.
	s.workers.Range(func(key, value any) bool {
		w := value.(*worker)
		if subject, _ := key.(*gate).who(); subject != "bob" {
			return true
		}
		if ok, err := w.client.Methods.Notify(ctx, protocol.Progress{ProjectId: "7", Percent: 2}); err != nil || !ok {
			t.Errorf("reverse call under the client's trust of its server: %v", err)
		}
		_, err := w.client.Methods.Notify(ctx, protocol.Progress{ProjectId: "8", Percent: 2})
		expect(t, "reverse call the client's context does not cover", err, "auth.denied")
		return false
	})
	if bob.notify.Load() != 1 {
		t.Fatalf("reverse effects: %d", bob.notify.Load())
	}
}

// ---- AUTH-SEAM-003, the generic routes: S.Job and a closed generic callable ----------

// The holder family draws worker as S: a Held carries S.Job values, which are
// worker.Job with its guarded callables. Two exposures of the client — one
// guarded, one denied — export the same declared type; each reference keeps
// the treatment of the exposure that exported it, through the generic
// container, the wire and the self-reference on the way back.
type holding struct {
	invoked atomic.Int64
	seen    []error
	mu      sync.Mutex
}

func (h *holding) Exchange(ctx context.Context, held holder.Held[protocol.Job]) (holder.Held[protocol.Job], error) {
	// The owner invokes each supplied job's status: a reverse invocation
	// decided by the supplier's exposure under the supplier's context.
	for _, job := range append([]protocol.Job{held.Job}, held.Others...) {
		_, err := job.Status(ctx, "peek")
		h.mu.Lock()
		h.seen = append(h.seen, err)
		h.mu.Unlock()
		h.invoked.Add(1)
	}
	return held, nil
}

func TestAUTHSEAM003GenericRoutesKeepTheGuard(t *testing.T) {
	clock.Store(1000)
	ctx := testContext(t)
	h := &holding{}
	var serverScope *live.Scope
	handler, err := runtime.NewHandler(runtime.ServerOptions{
		Authenticate: func(r *http.Request) (context.Context, error) { return withGate(r.Context(), trusted("server")), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
			scope, err := live.Over(peer, live.Options{MaxImports: 32, MaxExports: 64})
			if err != nil {
				return err
			}
			serverScope = scope
			wire, err := holderbinding.ToWire[protocol.Job, protocol.Tag](func(holder.Client[protocol.Job]) (holder.Server[protocol.Job], error) {
				return holder.Server[protocol.Job]{Methods: h}, nil
			}, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)}, protocol.AdapterJob())
			if err != nil {
				return err
			}
			detach, err := runtime.ForwardWire(peer.Wire(), wire)
			if err != nil {
				return err
			}
			go func() { <-peer.Done(); detach(); _ = wire.Close(duplex.CodeNormal, "") }()
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	// The client's two exposures of worker's callables: guarded, and denied.
	surface := workerSurface(t)
	guardedPolicy := workerPolicy()
	deniedPolicy := workerPolicy()
	deniedPolicy.treatments["callable:JobStatus"] = treatment{denied, "", ""}
	expA, err := bind(surface, guardedPolicy)
	if err != nil {
		t.Fatal(err)
	}
	expB, err := bind(surface, deniedPolicy)
	if err != nil {
		t.Fatal(err)
	}
	var invocations atomic.Int64
	jobOf := func(exp *exposure, ref, scope string) protocol.Job {
		exp.export(ref, scope)
		return protocol.Job{
			ProjectId: "7",
			Status: func(ctx context.Context, id string) (string, error) {
				if err := exp.invoke(ctx, ref, "callable:JobStatus"); err != nil {
					return "", err
				}
				invocations.Add(1)
				return "ok:" + id, nil
			},
			Cancel: func(ctx context.Context, id string) (bool, error) {
				return false, public("auth.member_denied", "cancel")
			},
		}
	}
	var clientScope *live.Scope
	// The client trusts the server it dialed: "server" reads projects.
	peer, _, err := runtime.Dial(withGate(ctx, trusted("server")), "ws"+strings.TrimPrefix(server.URL, "http"), runtime.DialOptions{Options: runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
		clientScope, err = live.Over(p, live.Options{MaxImports: 32, MaxExports: 64})
		return err
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	model, err := holderbinding.FromWire[protocol.Job, protocol.Tag](ctx, peer.Wire(), runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(clientScope)}, protocol.AdapterJob())
	if err != nil {
		t.Fatal(err)
	}
	access, err := model(holder.Client[protocol.Job]{})
	if err != nil {
		t.Fatal(err)
	}
	owner := clientScope.Owner().Child()
	defer owner.Release()
	octx := live.WithOwner(ctx, owner)
	held := holder.Held[protocol.Job]{
		Job:    jobOf(expA, "a:7", "projects/7"),
		Others: []protocol.Job{jobOf(expB, "b:7", "projects/7"), jobOf(expA, "a:9", "projects/9")},
	}
	back, err := access.Methods.Exchange(octx, held)
	if err != nil {
		t.Fatal(err)
	}
	// The server, under its own context (server: read projects/7), was
	// admitted by exposure A for A's job on 7, refused by B's treatment of
	// the same declared type, and refused by A's job on 9 — its own
	// authority, not the supplier's, decides what it may invoke.
	if h.invoked.Load() != 3 || len(h.seen) != 3 {
		t.Fatalf("server invoked %d", h.invoked.Load())
	}
	if h.seen[0] != nil || code(h.seen[1]) != "auth.member_denied" || code(h.seen[2]) != "auth.denied" {
		t.Fatalf("server-side decisions: %v %v %v", h.seen[0], h.seen[1], h.seen[2])
	}
	// The references came back through the container as the server's
	// re-exports. Invoked here they travel client → server → this side's
	// exporter, which runs under the connection that carries the
	// invocation in — the server's — never under the local caller's
	// context, which does not cross a wire: A's job on 7 is admitted with
	// no local context at all, A's job on 9 is refused whatever the local
	// caller holds, and B's stays denied. Forwarding shows the forwarder.
	if _, err := back.Job.Status(ctx, "x"); err != nil {
		t.Fatalf("A's job on 7, forwarded, decided as the server: %v", err)
	}
	_, err = back.Others[1].Status(withGate(ctx, trusted("alice")), "x")
	expect(t, "A's job on 9, forwarded, decided as the server not the local caller", err, "auth.denied")
	_, err = back.Others[0].Status(withGate(ctx, trusted("alice")), "x")
	expect(t, "B's job under any context", err, "auth.member_denied")
	if protocol.ContractJobStatus != "worker/JobStatus" || back.Job.ProjectId != back.Others[0].ProjectId {
		t.Fatal("type identity changed with the exposure")
	}
	// The true self-reference: this side's own export imported back into
	// the same scope resolves to the function without a frame. The
	// optimization keeps the guard and decides on the invoking context,
	// because the closure is the guard — nothing was unwrapped.
	raw, err := live.ValueEnvironment(clientScope).Export(live.WithOwner(ctx, owner), func(ctx context.Context) (json.RawMessage, error) {
		return protocol.AdapterJob().Export(ctx, held.Job)
	})
	if err != nil {
		t.Fatal(err)
	}
	var self protocol.Job
	if err := live.ValueEnvironment(clientScope).Import(live.WithOwner(ctx, owner), func(ctx context.Context) error {
		self, err = protocol.AdapterJob().Import(ctx, raw)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := self.Status(withGate(ctx, trusted("alice")), "x"); err != nil {
		t.Fatalf("self-reference under a covering context: %v", err)
	}
	_, err = self.Status(withGate(ctx, trusted("carol")), "x")
	expect(t, "self-reference under a non-covering context", err, "auth.denied")
	_, err = self.Status(ctx, "x")
	expect(t, "self-reference without a context", err, "auth.unauthenticated")
	if invocations.Load() != 3 {
		t.Fatalf("client-side invocations: %d", invocations.Load())
	}
	owner.Release()
	await(t, "client scope drains", func() bool { return clientScope.Counts() == (live.Counts{}) })
	await(t, "server scope drains", func() bool { return serverScope.Counts() == (live.Counts{}) })

	t.Run("closed generic callable inside a generic container", func(t *testing.T) {
		// cell<T> with T = ops.Closed: the guarded closure goes in through
		// replace, comes back out through get, and is invoked by the owner
		// in between — every route meets the closure's own guard.
		var invoked atomic.Int64
		store := &memoryCell{}
		var sScope *live.Scope
		handler, err := runtime.NewHandler(runtime.ServerOptions{
			Authenticate: func(r *http.Request) (context.Context, error) { return withGate(r.Context(), trusted("server")), nil },
			CheckOrigin:  func(*http.Request) bool { return true },
			Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
				scope, err := live.Over(peer, live.Options{MaxImports: 32, MaxExports: 64})
				if err != nil {
					return err
				}
				sScope = scope
				wire, err := cellbinding.ToWire[ops.Closed](func(cell.Client[ops.Closed]) (cell.Server[ops.Closed], error) {
					return cell.Server[ops.Closed]{Methods: store}, nil
				}, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)}, ops.AdapterClosed())
				if err != nil {
					return err
				}
				detach, err := runtime.ForwardWire(peer.Wire(), wire)
				if err != nil {
					return err
				}
				go func() { <-peer.Done(); detach(); _ = wire.Close(duplex.CodeNormal, "") }()
				return nil
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(handler)
		defer server.Close()
		var cScope *live.Scope
		peer, _, err := runtime.Dial(withGate(ctx, trusted("server")), "ws"+strings.TrimPrefix(server.URL, "http"), runtime.DialOptions{Options: runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
			cScope, err = live.Over(p, live.Options{MaxImports: 32, MaxExports: 64})
			return err
		}}})
		if err != nil {
			t.Fatal(err)
		}
		defer peer.Close()
		model, err := cellbinding.FromWire[ops.Closed](ctx, peer.Wire(), runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(cScope)}, ops.AdapterClosed())
		if err != nil {
			t.Fatal(err)
		}
		access, err := model(cell.Client[ops.Closed]{})
		if err != nil {
			t.Fatal(err)
		}
		exp, _ := bind(workerSurface(t), workerPolicy())
		exp.export("closed:7", "projects/7")
		exp.export("closed:9", "projects/9")
		closedOn := func(ref string) ops.Closed {
			return func(ctx context.Context, in string) (string, error) {
				if err := exp.invoke(ctx, ref, "callable:JobStatus"); err != nil {
					return "", err
				}
				invoked.Add(1)
				return ref + ":" + in, nil
			}
		}
		owner := cScope.Owner().Child()
		defer owner.Release()
		octx := live.WithOwner(ctx, owner)
		if _, err := access.Methods.Replace(octx, cell.Put[ops.Closed]{Value: closedOn("closed:7")}); err != nil {
			t.Fatal(err)
		}
		if store.seen == nil || *store.seen != nil {
			t.Fatalf("the owner's invocation on arrival, under its context: %v", store.seen)
		}
		got, err := access.Methods.Get(octx)
		if err != nil {
			t.Fatal(err)
		}
		// Back out of the container as the server's re-export: invoked here
		// it is decided at this side's exporter under the server's
		// connection — admitted for 7 with no local context at all.
		if out, err := got(ctx, "x"); err != nil || out != "closed:7:x" {
			t.Fatalf("returned closed callable, forwarded, decided as the server: %q %v", out, err)
		}
		// The same container, a closure exported under a scope the server
		// does not cover: refused on arrival and refused on the way back,
		// whatever the local caller holds.
		if _, err := access.Methods.Replace(octx, cell.Put[ops.Closed]{Value: closedOn("closed:9")}); err != nil {
			t.Fatal(err)
		}
		expect(t, "the owner's invocation on arrival, outside its authority", *store.seen, "auth.denied")
		got, err = access.Methods.Get(octx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = got(withGate(ctx, trusted("alice")), "x")
		expect(t, "returned closed callable on 9, forwarded, decided as the server not the local caller", err, "auth.denied")
		if invoked.Load() != 2 {
			t.Fatalf("invocations: %d", invoked.Load())
		}
		store.release()
		owner.Release()
		await(t, "client scope drains", func() bool { return cScope.Counts() == (live.Counts{}) })
		await(t, "server scope drains", func() bool { return sScope.Counts() == (live.Counts{}) })
	})
}

// memoryCell is a reusable owner that knows only T; it retains the incoming
// lifetime with the value and invokes what it was given, once, on arrival.
type memoryCell struct {
	mu     sync.Mutex
	value  ops.Closed
	owners []*live.Owner
	seen   *error
}

func (c *memoryCell) Replace(ctx context.Context, input cell.Put[ops.Closed]) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	owner, ok := live.OwnerOf(ctx)
	if !ok {
		return 0, errors.New("replace has no active value lifetime")
	}
	c.owners = append(c.owners, owner)
	c.value = input.Value
	_, err := input.Value(ctx, "arrival")
	c.seen = &err
	return 1, nil
}

func (c *memoryCell) Get(ctx context.Context) (ops.Closed, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if owner, ok := live.OwnerOf(ctx); ok {
		c.owners = append(c.owners, owner)
	}
	return c.value, nil
}

func (c *memoryCell) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range c.owners {
		_ = o.Release()
	}
	c.owners = nil
}

// ---- AUTH-SEAM-004: emissions and replays are decided before they leave ---------------

func TestAUTHSEAM004EmissionsAreGuarded(t *testing.T) {
	clock.Store(1000)
	s := serve(t, workerPolicy())
	ctx := testContext(t)
	bob := dial(t, ctx, s.url())
	carol := dial(t, ctx, s.url())
	none := dial(t, ctx, s.url())
	if err := bob.prove(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := carol.prove(ctx, "carol"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*client{bob, carol, none} {
		if _, err := c.access.Methods.Whoami(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// One emission toward three connections: decided per recipient at
	// emission time; bob alone reads projects/7, and a refused recipient
	// is not told.
	delivered, dropped := s.emitAll(protocol.Progress{ProjectId: "7", Percent: 50})
	if delivered != 1 || dropped != 2 {
		t.Fatalf("delivered %d dropped %d", delivered, dropped)
	}
	await(t, "bob receives", func() bool { return bob.received() == 1 })
	if carol.received() != 0 || none.received() != 0 || s.counters.emissions.Load() != 1 {
		t.Fatalf("received carol=%d none=%d emitted=%d", carol.received(), none.received(), s.counters.emissions.Load())
	}
	// A subscriber whose authority expires stops receiving at the next
	// emission, with the connection open and the subscription intact.
	clock.Store(2000)
	if delivered, dropped := s.emitAll(protocol.Progress{ProjectId: "7", Percent: 60}); delivered != 0 || dropped != 3 {
		t.Fatalf("after expiry: delivered %d dropped %d", delivered, dropped)
	}
	clock.Store(1000)
	if delivered, _ := s.emitAll(protocol.Progress{ProjectId: "7", Percent: 70}); delivered != 1 {
		t.Fatalf("after the clock returns: delivered %d", delivered)
	}
	await(t, "bob receives again", func() bool { return bob.received() == 2 })

	t.Run("a replayed log is decided at each disclosure", func(t *testing.T) {
		// The recorder appends typed events to a consumer log; Follow
		// replays them to a target wire. The target the owner hands to
		// Follow is a guarding wire: each replayed event frame is decided
		// against the recipient's context at replay time, delivered or
		// dropped — never inherited from the moment of subscribing.
		origin, sink, err := runtime.NewWirePair(runtime.Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer sink.Close(duplex.CodeNormal, "")
		log := duplex.NewMemoryWireLog()
		recorder, err := binding.Record(ctx, origin, log, duplex.RecordOptions{}, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(bob.scope)})
		if err != nil {
			t.Fatal(err)
		}
		defer recorder.Close(duplex.CodeNormal, "")
		for _, p := range []protocol.Progress{{ProjectId: "7", Percent: 10}, {ProjectId: "8", Percent: 20}, {ProjectId: "7", Percent: 30}} {
			if err := recorder.Append(ctx, binding.RecordedProgress{Data: p}); err != nil {
				t.Fatal(err)
			}
		}
		await(t, "log head", func() bool { head, _ := log.Head(ctx); return head == 3 })
		type replayed struct {
			g                  *gate
			delivered, dropped int
		}
		var targets []*replayed
		s.peers.Range(func(key, _ any) bool {
			targets = append(targets, &replayed{g: key.(*gate)})
			return true
		})
		// Replay toward each connection through its guard.
		var deliveredTotal, droppedTotal int
		exp := s.exposure
		for _, r := range targets {
			r := r
			target := &guardingWire{Wire: serverPeerWire(t, s, r.g), decide: func(data json.RawMessage) bool {
				var p protocol.Progress
				if json.Unmarshal(data, &p) != nil {
					return false
				}
				err := exp.decide(withGate(context.Background(), r.g), "event:progress", map[string]any{"projectId": p.ProjectId}, "")
				if err != nil {
					r.dropped++
					return false
				}
				r.delivered++
				return true
			}}
			follower, err := recorder.Follow(ctx, 0, target)
			if err != nil {
				t.Fatal(err)
			}
			await(t, "replay completes", func() bool { return r.delivered+r.dropped == 3 })
			follower.Close()
			deliveredTotal += r.delivered
			droppedTotal += r.dropped
		}
		// bob: 7,7 delivered and 8 dropped; carol and none: all dropped.
		if deliveredTotal != 2 || droppedTotal != 7 {
			t.Fatalf("replay delivered %d dropped %d", deliveredTotal, droppedTotal)
		}
		await(t, "bob receives the replay", func() bool { return bob.received() == 4 })
		if carol.received() != 0 || none.received() != 0 {
			t.Fatalf("replay reached carol=%d none=%d", carol.received(), none.received())
		}
	})
}

// serverPeerWire finds the server-side peer of the connection whose gate this is.
func serverPeerWire(t *testing.T, s *served, g *gate) bitwire.Wire {
	t.Helper()
	value, ok := s.peers.Load(g)
	if !ok {
		t.Fatal("no server peer for this gate")
	}
	return value.(*runtime.Peer).Wire()
}

// guardingWire decides each event frame before it reaches the wire it wraps;
// every other frame passes. It is the owner's composition over the public
// Wire, placed where a replay meets a recipient.
type guardingWire struct {
	bitwire.Wire
	decide func(data json.RawMessage) bool
}

func (w *guardingWire) Send(path []string, message bitwire.Message) error {
	if message.Frame.Kind == bitwire.ProfileEvent && !w.decide(message.Frame.Data) {
		return nil
	}
	return w.Wire.Send(path, message)
}

// ---- AUTH-SEAM-005: expiry at use, nothing else disturbed --------------------------------

func TestAUTHSEAM005ExpiryAtUse(t *testing.T) {
	clock.Store(1000)
	s := serve(t, workerPolicy())
	ctx := testContext(t)
	bob := dial(t, ctx, s.url())
	alice := dial(t, ctx, s.url())
	if err := bob.prove(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := alice.prove(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	owner := bob.scope.Owner().Child()
	octx := live.WithOwner(ctx, owner)
	job, err := bob.access.Methods.Start(octx, protocol.StartRequest{ProjectId: "7"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Store(2000) // bob's authority is over; the transport is not
	_, err = bob.access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7"})
	expect(t, "expired at use", err, "auth.denied")
	_, err = job.Status(octx, "j1")
	expect(t, "a retained callable after expiry", err, "auth.denied")
	if bob.peer.Err() != nil {
		t.Fatalf("expiry ended the connection: %v", bob.peer.Err())
	}
	// Cancellation is the transport's, not authority's: a call the caller
	// cancels is cancelled whatever the clock says.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := bob.access.Methods.List(cancelled, protocol.ListRequest{ProjectId: "7"}); err == nil {
		t.Fatal("a cancelled call completed")
	}
	// An unrelated borrower is untouched.
	if _, err := alice.access.Methods.List(ctx, protocol.ListRequest{ProjectId: "7"}); err != nil {
		t.Fatalf("alice after bob's expiry: %v", err)
	}
	// Release and cleanup are the scope's: the owner releases, the scope
	// drains to zero, the connection closes normally.
	if err := owner.Release(); err != nil {
		t.Fatal(err)
	}
	await(t, "bob's scope drains", func() bool { return bob.scope.Counts() == (live.Counts{}) })
	if err := bob.peer.Close(); err != nil {
		t.Fatalf("close after expiry: %v", err)
	}

	t.Run("the decision at the effect", func(t *testing.T) {
		// Admitted at dispatch, refused at the effect: the world changed
		// between the early check and the owner's transaction — once the
		// clock, once the project's state — and the effect did not apply.
		clock.Store(1000)
		var change func()
		s := serve(t, workerPolicy(), func(s *served) { s.hook = func() { change() } })
		bob := dial(t, ctx, s.url())
		if err := bob.prove(ctx, "bob"); err != nil {
			t.Fatal(err)
		}
		owner := bob.scope.Owner().Child()
		defer owner.Release()
		octx := live.WithOwner(ctx, owner)
		change = func() { clock.Store(2000) }
		_, err := bob.access.Methods.Start(octx, protocol.StartRequest{ProjectId: "7"})
		expect(t, "authority expired between dispatch and effect", err, "auth.denied")
		clock.Store(1000)
		change = func() { s.state.archive("7") }
		_, err = bob.access.Methods.Start(octx, protocol.StartRequest{ProjectId: "7"})
		expect(t, "the owner's condition at the effect", err, protocol.ErrorArchived)
		if s.counters.dispatched.Load() != 2 || s.counters.effects.Load() != 0 {
			t.Fatalf("dispatched %d effects %d", s.counters.dispatched.Load(), s.counters.effects.Load())
		}
	})
}

// ---- AUTH-SEAM-006: the typed handshake and the exposure coexist --------------------------

func TestAUTHSEAM006HandshakeRefusalAndBindingCoexist(t *testing.T) {
	clock.Store(1000)
	s := serve(t, workerPolicy())
	ctx := testContext(t)
	// A client of another revision is refused at interpretation, before
	// any model dispatch; the presence of an exchange does not downgrade
	// that into a model call, and nothing ran.
	var otherScope *live.Scope
	peer, _, err := runtime.Dial(ctx, s.url(), runtime.DialOptions{Options: runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
		otherScope, err = live.Over(p, live.Options{})
		return err
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	_, err = binding2.FromWire(ctx, peer.Wire(), runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(otherScope)})
	expect(t, "another revision at interpretation", err, "contract_mismatch")
	// An undeclared member is refused by the wire, not routed anywhere.
	var out json.RawMessage
	err = runtime.CallWire(ctx, peer.Wire(), []string{"archive"}, protocol.ListRequest{ProjectId: "7"}, &out)
	expect(t, "an undeclared member", err, "method_not_found")
	if s.counters.dispatched.Load() != 0 {
		t.Fatal("a handler ran during a refused interpretation")
	}
	// The policy binds whole against its own surface. Against the new
	// revision's surface the member it gained has no treatment; a policy
	// for the other digest is a contract mismatch; a key the surface lacks
	// is undeclared; a hole no field fills is invalid.
	surface2, err := surfaceOf("worker2", "server", protocol2.WireDeclaration(), protocol2.WireDigest())
	if err != nil {
		t.Fatal(err)
	}
	p := workerPolicy()
	p.family, p.digest = "worker2", protocol2.WireDigest()
	_, err = bind(surface2, p)
	if err == nil || err.Error() != "auth.member_unbound: method:archive" {
		t.Fatalf("policy against a revision with a new member: %v", err)
	}
	_, err = bind(workerSurface(t), p)
	if err == nil || !strings.HasPrefix(err.Error(), "auth.contract_mismatch") {
		t.Fatalf("policy for another digest: %v", err)
	}
	extra := workerPolicy()
	extra.treatments["method:stop"] = treatment{denied, "", ""}
	_, err = bind(workerSurface(t), extra)
	if err == nil || err.Error() != "auth.member_undeclared: method:stop" {
		t.Fatalf("undeclared member: %v", err)
	}
	less := workerPolicy()
	delete(less.treatments, "callable:JobCancel")
	_, err = bind(workerSurface(t), less)
	if err == nil || err.Error() != "auth.member_unbound: callable:JobCancel" {
		t.Fatalf("unbound member: %v", err)
	}
	bad := workerPolicy()
	bad.treatments["method:list"] = treatment{guarded, "read", "projects/{owner}"}
	bad.treatments["event:progress"] = treatment{guarded, "read", "{export}"}
	_, err = bind(workerSurface(t), bad)
	if err == nil || err.Error() != "auth.template_invalid: event:progress,method:list" {
		t.Fatalf("invalid templates: %v", err)
	}
	// A server whose policy does not bind attaches nothing: every
	// connection is refused in Prepare, and no partial exposure exists.
	unbound := serve(t, less)
	if unbound.exposure != nil {
		t.Fatal("an unbound policy constructed an exposure")
	}
	refusedPeer, _, err := runtime.Dial(ctx, unbound.url(), runtime.DialOptions{})
	if err == nil {
		defer refusedPeer.Close()
		select {
		case <-refusedPeer.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("a connection to an unbound exposure stayed open")
		}
		err = runtime.CallWire(ctx, refusedPeer.Wire(), []string{"whoami"}, struct{}{}, &out)
		if err == nil {
			t.Fatal("a public member answered on an unbound exposure")
		}
	}
	await(t, "the connection was refused at preparation", func() bool { return unbound.refused.Load() >= 1 })
	if unbound.counters.dispatched.Load() != 0 {
		t.Fatal("an unbound exposure ran a handler")
	}
}

// ---- the Go↔TypeScript leg ------------------------------------------------------------------

// The TypeScript client runs AUTH-SEAM-001, -002 and -005 against the Go
// server over a real socket, and guards a supplied sink of its own; the
// server's emissions and clock are driven by the client through controls.
func TestAUTHSEAMTypeScriptClient(t *testing.T) {
	clock.Store(1000)
	s := serve(t, workerPolicy(), func(s *served) {
		s.controls = func(peer *runtime.Peer) error {
			if err := peer.Handle("test.emit", func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
				var p protocol.Progress
				if err := json.Unmarshal(raw, &p); err != nil {
					return nil, err
				}
				delivered, dropped := s.emitAll(p)
				return map[string]int{"delivered": delivered, "dropped": dropped}, nil
			}); err != nil {
				return err
			}
			return peer.Handle("test.clock", func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
				var at uint64
				if err := json.Unmarshal(raw, &at); err != nil {
					return nil, err
				}
				clock.Store(at)
				return at, nil
			})
		}
	})
	ctx := testContext(t)
	command := exec.CommandContext(ctx, "node", "--loader", "./runtime-loader.mjs", "seam.ts", s.url())
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("AUTH-SEAM typescript: %v\n%s", err, output)
	}
	t.Logf("%s", output)
	if s.counters.dispatched.Load() == 0 || s.counters.effects.Load() != 1 || s.counters.emissions.Load() != 1 {
		t.Fatalf("typescript leg: dispatched=%d effects=%d emissions=%d", s.counters.dispatched.Load(), s.counters.effects.Load(), s.counters.emissions.Load())
	}
}
