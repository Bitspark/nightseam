// Production adapter for independent Bitwire ADR0006 cases at fdc2ae99.
// The scenario harness is upstream; construction, parts and routing use Nightseam.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"time"

	wire "github.com/Bitspark/bitwire/wire/go"
	"github.com/Bitspark/nightseam/duplex/go"
	ns "github.com/Bitspark/nightseam/runtime/go"
)

// origin is a node's own value: behavior at its empty relative path. It never
// receives a path. Refusal is a value too, not a missing origin.
type origin struct {
	name     string
	instance int
	handle   func(wire.Message) error
}

func (o *origin) Send(path []string, message wire.Message) error {
	if len(path) != 0 {
		return errors.New("origin received a nonempty path")
	}
	return o.handle(message)
}

var refuse = &origin{handle: func(wire.Message) error { return duplex.ErrNoRoute }}

type entry struct {
	key   string
	child wire.Wire
}

// A realization constructs declared composites from their own value and
// complete child access. Parts are the construction owner's retained description.
type realization interface {
	compose(own *origin, entries []entry) (wire.Wire, error)
	parts(composite wire.Wire) (*origin, []entry, bool)
	expose(composite wire.Wire) wire.Wire
	teardown()
}

// Only the assembler retains descriptions; public access is the bound facade.
type production struct{ retained map[wire.Wire]duplex.Declared }

func (r *production) compose(own *origin, entries []entry) (wire.Wire, error) {
	children := make([]duplex.DeclaredChild, len(entries))
	for i, e := range entries {
		children[i] = duplex.DeclaredChild{Key: e.key, Wire: e.child}
	}
	d, err := duplex.ComposeDeclared(own, children)
	if err != nil {
		return nil, err
	}
	// Probe the production constructor's copy, not just the scenario adapter's.
	for i := range children {
		children[i] = duplex.DeclaredChild{}
	}
	access := d.Bind()
	r.retained[access] = d
	return access, nil
}
func (r *production) parts(w wire.Wire) (*origin, []entry, bool) {
	d, found := r.retained[w]
	if !found {
		return nil, nil, false
	}
	own, children := d.Decompose()
	entries := make([]entry, len(children))
	for i, child := range children {
		entries[i] = entry{child.Key, child.Wire}
	}
	return own.(*origin), entries, true
}
func (r *production) expose(w wire.Wire) wire.Wire { return w }

// Declared construction acquires no resource requiring teardown.
func (r *production) teardown() {}

type sendAccess struct {
	send func([]string, wire.Message) error
}

func (a *sendAccess) Send(p []string, m wire.Message) error { return a.send(p, m) }

// ---- Instrumented child access and interception used by the fixtures ----

type forwarded struct {
	path  []string
	count int
}
type env struct {
	sender    wire.Wire
	expected  *wire.Message
	contexts  map[*wire.ReturnAddress]*struct{}
	marker    *struct{}
	unchanged bool
	last      *forwarded
	trace     []any
	instances map[string]int
}

func (e *env) verify(m wire.Message) {
	e.unchanged = e.unchanged && e.expected != nil && reflect.DeepEqual(m.Frame, e.expected.Frame) &&
		m.Return == e.expected.Return && m.Return != nil && e.contexts[m.Return] == e.marker
}
func (e *env) next(name string) int {
	e.instances[name]++
	return e.instances[name]
}
func (e *env) newOrigin(name string) *origin {
	o := &origin{name: name, instance: e.next(name)}
	count := 0
	o.handle = func(m wire.Message) error {
		e.verify(m)
		count++
		e.last = &forwarded{[]string{name}, count}
		return duplex.At(e.sender, []string{name}).Send(nil, m)
	}
	return o
}

// access is complete, stateful child access with its own instance counter.
type access struct {
	name     string
	instance int
	count    int
	env      *env
}

func (e *env) newAccess(name string) *access { return &access{name, e.next(name), 0, e} }
func (a *access) Send(p []string, m wire.Message) error {
	a.env.verify(m)
	a.count++
	a.env.last = &forwarded{append([]string{a.name}, p...), a.count}
	return duplex.At(a.env.sender, []string{a.name}).Send(p, m)
}

type policy struct {
	id                         string
	instance, limit, remaining int
}

// guard is interception composed around access; it is not a node value.
type guard struct {
	policy *policy
	inner  wire.Wire
	env    *env
}

func (g *guard) Send(p []string, m wire.Message) error {
	g.env.trace = append(g.env.trace, []any{"check", g.policy.id, append([]string{}, p...)})
	if g.policy.remaining == 0 {
		return errors.New("guard refused")
	}
	if g.policy.remaining > 0 {
		g.policy.remaining--
	}
	return g.inner.Send(p, m)
}

// ---- Fixture interpretation ----

type declaration struct {
	ID       string
	Origin   *string
	Children [][2]string
	Access   string
}
type step struct {
	Op         string
	Target     []string `json:"path"`
	Keep       [][]string
	Selections [][]string
	Via, Key   string
	To, Node   string
	Mode, ID   string
	Origin     *string
	Limit      int
}

type testCase struct {
	ID, Kind, Root, Fault string
	Steps                 []step
	Relay, Mount          bool
}
type input struct {
	Declarations []declaration
	Cases        []testCase
}

type harness struct {
	R          realization
	env        *env
	decls      map[string]declaration
	built      map[string]wire.Wire
	origins    map[string]*origin
	root, view wire.Wire
	partsExact bool
}

func sameEntries(got, want []entry) bool {
	if len(got) != len(want) {
		return false
	}
	index := map[string]wire.Wire{}
	for _, e := range want {
		index[e.key] = e.child
	}
	for _, e := range got {
		child, found := index[e.key]
		if !found || child != e.child {
			return false
		}
		delete(index, e.key)
	}
	return len(index) == 0
}

// construct records R1 for every composite and mutates the caller's input
// afterward: the composite must retain its own copy of the description.
func (h *harness) construct(own *origin, entries []entry) (wire.Wire, error) {
	input := append([]entry{}, entries...)
	w, err := h.R.compose(own, input)
	if err != nil {
		return nil, err
	}
	for i := range input {
		input[i] = entry{"mutated", nil}
	}
	o, got, ok := h.R.parts(w)
	exact := ok && o == own && sameEntries(got, entries)
	if exact && len(got) > 0 {
		got[0].child = nil // Returned parts are a copy of the retained description.
		o, got, ok = h.R.parts(w)
		exact = ok && o == own && sameEntries(got, entries)
	}
	h.partsExact = h.partsExact && exact
	return w, nil
}
func (h *harness) compose(own *origin, entries []entry) wire.Wire {
	w, err := h.construct(own, entries)
	check(err)
	return w
}
func (h *harness) originNamed(name string) *origin {
	if o := h.origins[name]; o != nil {
		return o
	}
	o := h.env.newOrigin(name)
	h.origins[name] = o
	return o
}
func (h *harness) build(id string, fault string, rootID string, visiting map[string]bool) (wire.Wire, error) {
	if w := h.built[id]; w != nil {
		return w, nil
	}
	d, found := h.decls[id]
	if !found {
		return nil, errors.New("missing declaration")
	}
	if d.Access != "" {
		w := h.env.newAccess(d.Access)
		h.built[id] = w
		return w, nil
	}
	if visiting[id] {
		return nil, errors.New("cyclic declaration")
	}
	visiting[id] = true
	children := d.Children
	if id == rootID && fault == "cycle" {
		children = append(append([][2]string{}, children...), [2]string{"loop", rootID})
	}
	entries := []entry{}
	for _, c := range children {
		child, err := h.build(c[1], fault, rootID, visiting)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{c[0], child})
	}
	if id == rootID {
		switch fault {
		case "duplicate":
			leaf, err := h.build("leaf", "", rootID, visiting)
			check(err)
			entries = append(entries, entry{"a", leaf})
		case "invalidKey":
			leaf, err := h.build("leaf", "", rootID, visiting)
			check(err)
			entries = append(entries, entry{string([]byte{0xff}), leaf})
		case "missingChild":
			entries = append(entries, entry{"hole", nil})
		}
	}
	own := refuse
	if d.Origin != nil {
		own = h.originNamed(*d.Origin)
	}
	w, err := h.construct(own, entries)
	if err != nil {
		return nil, err
	}
	delete(visiting, id)
	h.built[id] = w
	return w, nil
}

func (h *harness) caller() wire.Wire {
	if g, ok := h.root.(*guard); ok {
		return g
	}
	return h.R.expose(h.root)
}
func renderOrigin(o *origin) any {
	if o == refuse {
		return nil
	}
	return []any{o.name, o.instance}
}
func (h *harness) render(w wire.Wire) any {
	if own, entries, ok := h.R.parts(w); ok {
		sort.Slice(entries, func(i, j int) bool { return bytes.Compare([]byte(entries[i].key), []byte(entries[j].key)) < 0 })
		children := []any{}
		for _, e := range entries {
			children = append(children, []any{e.key, h.render(e.child)})
		}
		return map[string]any{"origin": renderOrigin(own), "children": children}
	}
	switch v := w.(type) {
	case *access:
		return map[string]any{"access": []any{v.name, v.instance}}
	case *guard:
		return map[string]any{"guard": []any{v.policy.id, v.policy.instance}, "inner": h.render(v.inner)}
	}
	return "opaque"
}
func (h *harness) structure(w wire.Wire, path []string) any {
	if len(path) == 0 {
		return h.render(w)
	}
	_, entries, ok := h.R.parts(w)
	if !ok {
		panic("structure path leaves the declared composites")
	}
	for _, e := range entries {
		if e.key == path[0] {
			return h.structure(e.child, path[1:])
		}
	}
	return "missing"
}

// replaceAt rebuilds the declared ancestors of path from their retained parts.
// A guard retains its original policy instance around a rebuilt inner access.
func (h *harness) replaceAt(w wire.Wire, path []string, f func(wire.Wire) wire.Wire) wire.Wire {
	if g, ok := w.(*guard); ok && len(path) > 0 {
		return &guard{g.policy, h.replaceAt(g.inner, path, f), h.env}
	}
	if len(path) == 0 {
		return f(w)
	}
	own, entries, ok := h.R.parts(w)
	if !ok {
		panic("edit path leaves the declared composites")
	}
	for i := range entries {
		if entries[i].key == path[0] {
			entries[i].child = h.replaceAt(entries[i].child, path[1:], f)
		}
	}
	return h.compose(own, entries)
}
func (h *harness) rootComposite(f func(wire.Wire) wire.Wire) {
	if g, ok := h.root.(*guard); ok {
		h.root = &guard{g.policy, f(g.inner), h.env}
		return
	}
	h.root = f(h.root)
}
func containsPath(paths [][]string, path []string) bool {
	for _, p := range paths {
		if reflect.DeepEqual(append([]string{}, p...), append([]string{}, path...)) {
			return true
		}
	}
	return false
}

// rebuild performs one complete cut: kept subtrees are reused whole, and every
// other declared composite is rebuilt from its retained parts.
func (h *harness) rebuild(w wire.Wire, at []string, keep [][]string) wire.Wire {
	if containsPath(keep, at) {
		return w
	}
	if g, ok := w.(*guard); ok {
		return &guard{g.policy, h.rebuild(g.inner, at, keep), h.env}
	}
	own, entries, ok := h.R.parts(w)
	if !ok {
		return w // Opaque child access is retained whole.
	}
	for i := range entries {
		entries[i].child = h.rebuild(entries[i].child, append(append([]string{}, at...), entries[i].key), keep)
	}
	return h.compose(own, entries)
}
func (h *harness) copySubtree(w wire.Wire) wire.Wire {
	if a, ok := w.(*access); ok {
		return h.env.newAccess(a.name)
	}
	own, entries, ok := h.R.parts(w)
	if !ok {
		panic("cannot copy opaque access")
	}
	if own != refuse {
		own = h.env.newOrigin(own.name)
	}
	for i := range entries {
		entries[i].child = h.copySubtree(entries[i].child)
	}
	return h.compose(own, entries)
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
func wait[T any](ch <-chan T) T {
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		panic("delivery deadline exceeded")
	}
}

type scope struct{ cleanups []func() }

func (s *scope) close() {
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		s.cleanups[i]()
	}
}
func (s *scope) pair() (wire.Endpoint, wire.Endpoint) {
	if os.Getenv("NIGHTSEAM_BITWIRE_CARRIER") == "local" {
		a, b, err := ns.NewWirePair(ns.Options{})
		check(err)
		s.cleanups = append(s.cleanups, func() { _ = a.Close(duplex.CodeNormal, "done"); _ = b.Close(duplex.CodeNormal, "done") })
		return a, b
	}
	connected := make(chan *ns.Peer, 1)
	handler, err := ns.NewHandler(ns.ServerOptions{Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil }, CheckOrigin: func(*http.Request) bool { return true }, OnConnect: func(p *ns.Peer) { connected <- p }})
	check(err)
	server := httptest.NewServer(handler)
	s.cleanups = append(s.cleanups, server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	s.cleanups = append(s.cleanups, cancel)
	client, _, err := ns.Dial(ctx, server.URL, ns.DialOptions{ConnectTimeout: 5 * time.Second})
	check(err)
	remote := wait(connected)
	s.cleanups = append(s.cleanups, func() { _ = client.Close(); _ = remote.Close() })
	if os.Getenv("NIGHTSEAM_BITWIRE_REVERSE") == "1" {
		return remote.Wire(), client.Wire()
	}
	return client.Wire(), remote.Wire()
}
func event(value string) wire.Message {
	data, err := json.Marshal(value)
	check(err)
	return wire.Message{Frame: wire.ProfileFrame{Version: 1, Kind: wire.ProfileEvent, Data: data}}
}

type delivery struct {
	path    []string
	message wire.Message
}

var refuseWire wire.Wire = &sendAccess{func([]string, wire.Message) error { return duplex.ErrNoRoute }}

func (h *harness) send(w wire.Wire, path []string, message wire.Message, deliveries <-chan delivery) {
	h.env.expected, h.env.last = &message, nil
	if err := w.Send(path, message); err != nil {
		if h.env.last != nil {
			panic("a destination accepted a refused send")
		}
		h.env.trace = append(h.env.trace, []any{"refused"})
		return
	}
	got := wait(deliveries)
	if !bytes.Equal(got.message.Frame.Data, message.Frame.Data) || got.message.Frame.Kind != message.Frame.Kind {
		panic("message changed or unexpected delivery")
	}
	if h.env.last == nil || !reflect.DeepEqual(got.path, h.env.last.path) {
		panic("delivery does not match its declared destination")
	}
	h.env.trace = append(h.env.trace, []any{"delivered", got.path, h.env.last.count})
}
func (h *harness) marked(value string) wire.Message {
	m := event(value)
	m.Return = &wire.ReturnAddress{Wire: refuseWire}
	h.env.contexts[m.Return] = h.env.marker
	return m
}
func borrowed(sender wire.Wire, deliveries <-chan delivery) bool {
	if sender.Send([]string{"borrowed"}, event("borrowed")) != nil {
		return false
	}
	got := wait(deliveries)
	return reflect.DeepEqual(got.path, []string{"borrowed"}) && string(got.message.Frame.Data) == `"borrowed"`
}

func newHarness(R realization, fixture input, sender wire.Wire) *harness {
	h := &harness{R: R, decls: map[string]declaration{}, built: map[string]wire.Wire{}, origins: map[string]*origin{}, partsExact: true}
	h.env = &env{sender: sender, contexts: map[*wire.ReturnAddress]*struct{}{}, marker: &struct{}{}, unchanged: true, trace: []any{}, instances: map[string]int{}}
	for _, d := range fixture.Declarations {
		if _, found := h.decls[d.ID]; found {
			panic("duplicate declaration")
		}
		h.decls[d.ID] = d
	}
	return h
}
func refusal(err error) any {
	return map[string]any{"construction": "refused"}
}
func (h *harness) node(id string) wire.Wire {
	w, err := h.build(id, "", "", map[string]bool{})
	check(err)
	return w
}

func (h *harness) apply(t testCase, index int, s step, deliveries <-chan delivery) {
	switch s.Op {
	case "send", "invalidPath":
		w := h.caller()
		if s.Via == "view" {
			if h.view == nil {
				panic("no captured view")
			}
			w = h.view
		}
		for _, prefix := range s.Selections {
			w = duplex.At(w, prefix)
		}
		path := s.Target
		if s.Op == "invalidPath" {
			path = []string{string([]byte{0xff})}
		}
		h.send(w, path, h.marked(fmt.Sprintf("%s:%d", t.ID, index)), deliveries)
	case "direct":
		h.send(h.node(s.Node), s.Target, h.marked(fmt.Sprintf("%s:%d", t.ID, index)), deliveries)
	case "structure":
		root := h.root
		if g, ok := root.(*guard); ok && len(s.Target) > 0 {
			root = g.inner
		}
		h.env.trace = append(h.env.trace, []any{"structure", h.structure(root, s.Target)})
	case "rebuild":
		h.root = h.rebuild(h.root, []string{}, s.Keep)
	case "origin":
		h.root = h.replaceAt(h.root, s.Target, func(w wire.Wire) wire.Wire {
			own, entries, ok := h.R.parts(w)
			if !ok {
				panic("origin of opaque access")
			}
			if s.Origin == nil {
				own = refuse
			} else {
				own = h.env.newOrigin(own.name)
			}
			return h.compose(own, entries)
		})
	case "substitute":
		h.root = h.replaceAt(h.root, s.Target, func(w wire.Wire) wire.Wire {
			if s.Mode == "copy" {
				return h.copySubtree(w)
			}
			return h.rebuild(w, []string{}, nil)
		})
	case "omit", "rename", "add":
		h.root = h.replaceAt(h.root, s.Target, func(w wire.Wire) wire.Wire {
			own, entries, ok := h.R.parts(w)
			if !ok {
				panic("edit of opaque access")
			}
			next := []entry{}
			for _, e := range entries {
				if e.key == s.Key && s.Op == "omit" {
					continue
				}
				if e.key == s.Key && s.Op == "rename" {
					e.key = s.To
				}
				next = append(next, e)
			}
			if s.Op == "add" {
				next = append(next, entry{s.Key, h.node(s.Node)})
			}
			return h.compose(own, next)
		})
	case "replace":
		h.root = h.replaceAt(h.root, s.Target, func(wire.Wire) wire.Wire { return h.node(s.Node) })
	case "view":
		h.view = h.caller()
		for _, prefix := range s.Selections {
			h.view = duplex.At(h.view, prefix)
		}
	case "guard":
		h.root = &guard{&policy{s.ID, h.env.next("policy:" + s.ID), s.Limit, s.Limit}, h.root, h.env}
	case "freshGuard":
		g := h.root.(*guard)
		h.root = &guard{&policy{g.policy.id, h.env.next("policy:" + g.policy.id), g.policy.limit, g.policy.limit}, g.inner, h.env}
	case "guardChild":
		h.root = h.replaceAt(h.root, append(append([]string{}, s.Target...), s.Key), func(w wire.Wire) wire.Wire {
			return &guard{&policy{s.ID, h.env.next("policy:" + s.ID), s.Limit, s.Limit}, w, h.env}
		})
	case "rebuildFromViews":
		g := h.root.(*guard)
		own, entries, ok := h.R.parts(g.inner)
		if !ok {
			panic("guarded access is not a composite")
		}
		for i := range entries {
			entries[i].child = duplex.At(g, []string{entries[i].key})
		}
		h.root = &guard{g.policy, h.compose(own, entries), h.env}
	case "teardown":
		h.R.teardown()
	default:
		panic("unknown step: " + s.Op)
	}
}

func (s *scope) carriers(t testCase) (wire.Endpoint, wire.Wire, wire.Endpoint) {
	source, receiver := s.pair()
	var sender wire.Wire = source
	if t.Mount {
		mounted := duplex.Mount(map[string]wire.Endpoint{"mounted": source})
		sender = duplex.At(mounted, []string{"mounted"})
		s.cleanups = append(s.cleanups, func() { _ = mounted.Close(duplex.CodeNormal, "done") })
	}
	if t.Relay {
		outgoing, target := s.pair()
		off, err := ns.ForwardWire(receiver, outgoing)
		check(err)
		s.cleanups = append(s.cleanups, off)
		receiver = target
	}
	return source, sender, receiver
}

func observe(R realization, fixture input, t testCase) any {
	s := &scope{}
	defer s.close()
	source, sender, receiver := s.carriers(t)
	deliveries := make(chan delivery, 64)
	off, err := receiver.Receive(wire.Receiver{Message: func(p []string, m wire.Message) {
		deliveries <- delivery{append([]string{}, p...), m}
	}})
	check(err)
	s.cleanups = append(s.cleanups, off)
	h := newHarness(R, fixture, sender)
	root, err := h.build(t.Root, t.Fault, t.Root, map[string]bool{})
	if err != nil {
		return refusal(err)
	}
	h.root = root
	for index, action := range t.Steps {
		h.apply(t, index, action, deliveries)
	}
	_, endpoint := h.caller().(wire.Endpoint)
	h.R.teardown()
	return map[string]any{"trace": h.env.trace, "partsExact": h.partsExact, "unchanged": h.env.unchanged, "sendOnly": !endpoint, "borrowedUsable": borrowed(source, deliveries)}
}

// pending admits a real request through the composite, then rebuilds, rebinds
// and tears the composition down before its late reply and captured cancel.
func pending(R realization, fixture input, t testCase) any {
	s := &scope{}
	defer s.close()
	source, sender, receiver := s.carriers(t)
	captured := make(chan wire.Message, 1)
	cancelled := make(chan string, 1)
	deliveries := make(chan delivery, 4)
	replies := make(chan string, 1)
	off, err := receiver.Receive(wire.Receiver{Message: func(p []string, m wire.Message) {
		if m.Frame.Kind == wire.ProfileEvent {
			deliveries <- delivery{append([]string{}, p...), m}
			return
		}
		lifecycle := m.Return.Wire
		check(lifecycle.Send([]string{"invocation.capture", "old"}, wire.Message{Frame: wire.ProfileFrame{Version: 1, Kind: wire.ProfileEvent, Data: json.RawMessage("null")}, Return: &wire.ReturnAddress{Wire: &sendAccess{func(p []string, m wire.Message) error {
			if len(p) != 0 || m.Frame.Kind != wire.ProfileCancel {
				panic("invalid captured control")
			}
			cancelled <- "old"
			return nil
		}}}}))
		check(lifecycle.Send([]string{"invocation.ready", "old"}, event("ready")))
		check(lifecycle.Send([]string{"invocation.begin", "old"}, event("begin")))
		captured <- m
	}})
	check(err)
	s.cleanups = append(s.cleanups, off)
	h := newHarness(R, fixture, sender)
	root, err := h.build(t.Root, "", t.Root, map[string]bool{})
	if err != nil {
		return refusal(err)
	}
	h.root = root
	original := &wire.ReturnAddress{Wire: &sendAccess{func(p []string, m wire.Message) error {
		if len(p) != 0 || m.Frame.Kind != wire.ProfileResponse || m.Frame.Error != nil {
			panic("unexpected reply")
		}
		var result string
		check(json.Unmarshal(m.Frame.Result, &result))
		replies <- result
		return nil
	}}}
	request := wire.Message{Frame: wire.ProfileFrame{Version: 1, Kind: wire.ProfileRequest, ID: "c:1", Params: json.RawMessage("null")}, Return: original}
	h.env.contexts[original] = h.env.marker
	h.env.expected = &request
	check(duplex.At(duplex.At(h.caller(), []string{"a"}), []string{"b"}).Send(nil, request))
	old := wait(captured)
	h.root = h.rebuild(h.root, []string{}, nil)
	for _, path := range [][]string{{"a", "b"}, {"alias"}} {
		h.apply(t, 0, step{Op: "replace", Target: path, Node: "replacement"}, deliveries)
	}
	h.send(duplex.At(h.caller(), []string{"alias"}), nil, h.marked("new"), deliveries)
	h.R.teardown()
	check(old.Return.Wire.Send(nil, wire.Message{Frame: wire.ProfileFrame{Version: 1, Kind: wire.ProfileResponse, ID: old.Frame.ID, Result: json.RawMessage(`"old"`)}}))
	late := wait(replies)
	check(old.Return.Wire.Send([]string{"invocation.control"}, wire.Message{Frame: wire.ProfileFrame{Version: 1, Kind: wire.ProfileCancel, ID: old.Frame.ID}}))
	controls := []string{wait(cancelled)}
	check(old.Return.Wire.Send([]string{"invocation.release", "old"}, event("release")))
	check(old.Return.Wire.Send([]string{"invocation.done", "old"}, event("done")))
	return map[string]any{"trace": h.env.trace, "lateReply": late, "cancelled": controls, "unchanged": h.env.unchanged, "borrowedUsable": borrowed(source, deliveries)}
}

func main() {
	if os.Getenv("NIGHTSEAM_BITWIRE_CARRIER") != "local" && os.Getenv("NIGHTSEAM_BITWIRE_CARRIER") != "peer" {
		panic("expected local or peer carrier")
	}
	if len(os.Args) != 2 {
		panic("usage: declared inputs.json")
	}
	data, err := os.ReadFile(os.Args[1])
	check(err)
	var fixture input
	check(json.Unmarshal(data, &fixture))
	output := []any{}
	for _, t := range fixture.Cases {
		R := &production{retained: map[wire.Wire]duplex.Declared{}}
		var observations any
		if t.Kind == "pending" {
			observations = pending(R, fixture, t)
		} else {
			observations = observe(R, fixture, t)
		}
		output = append(output, map[string]any{"id": t.ID, "observations": observations})
	}
	check(json.NewEncoder(os.Stdout).Encode(output))
}
