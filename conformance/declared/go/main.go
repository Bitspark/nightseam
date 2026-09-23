// Production adapter for the independent Bitwire ADR0005 cases at 671b61d.
// Derived from its scenario driver; routing, construction and parts are Nightseam's.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"time"

	wire "github.com/Bitspark/bitwire/wire/go"
	"github.com/Bitspark/nightseam/duplex/go"
	ns "github.com/Bitspark/nightseam/runtime/go"
)

type access struct {
	send func([]string, wire.Message) error
}

func (a *access) Send(p []string, m wire.Message) error { return a.send(p, m) }

type policy struct {
	id        string
	remaining int
	checks    *[][2]any
}

func (g *policy) check(p []string) error {
	if g == nil {
		return nil
	}
	*g.checks = append(*g.checks, [2]any{g.id, append([]string{}, p...)})
	if g.remaining == 0 {
		return fmt.Errorf("policy refused")
	}
	if g.remaining > 0 {
		g.remaining--
	}
	return nil
}

func (g *policy) Admit(p []string, _ wire.Message) error { return g.check(p) }

type entry = duplex.DeclaredChild
type declaration = duplex.Declared

func compose(own wire.Wire, gate *policy, entries []entry) (declaration, error) {
	var admission duplex.AdmissionPolicy = duplex.PermitAdmission{}
	if gate != nil {
		admission = gate
	}
	return duplex.ComposeDeclared(duplex.DeclaredValue{Own: own, Policy: admission}, entries)
}
func mustBuild(own wire.Wire, gate *policy, entries []entry) declaration {
	d, err := compose(own, gate, entries)
	check(err)
	return d
}
func parts(d declaration) (wire.Wire, *policy, []entry) {
	value, children := d.Decompose()
	gate, _ := value.Policy.(*policy)
	return value.Own, gate, children
}
func ownOf(d declaration) wire.Wire { value, _ := d.Decompose(); return value.Own }
func gateOf(d declaration) *policy  { _, gate, _ := parts(d); return gate }
func rebuild(d declaration, deep bool, memo map[declaration]declaration) declaration {
	if found, ok := memo[d]; ok {
		return found
	}
	own, gate, children := parts(d)
	if deep {
		for i, e := range children {
			children[i].Node = rebuild(e.Node, true, memo)
		}
	}
	next := mustBuild(own, gate, children)
	memo[d] = next
	return next
}
func retained(d declaration) bool {
	value, children := d.Decompose()
	other, err := duplex.ComposeDeclared(value, children)
	check(err)
	copied, entries := other.Decompose()
	if value.Own != copied.Own || value.Policy != copied.Policy || len(children) != len(entries) {
		return false
	}
	for i, child := range children {
		if child.Key != entries[i].Key || child.Node != entries[i].Node {
			return false
		}
	}
	if len(children) > 0 {
		children[0].Node = declaration{}
	}
	_, original := d.Decompose()
	return reflect.DeepEqual(original, entries)
}
func bind(d declaration) wire.Wire { return d.Bind() }

var refuse wire.Wire = &access{func([]string, wire.Message) error { return duplex.ErrNoRoute }}

type nodeSpec struct {
	ID       string
	Own      *string
	Policy   string
	Children [][2]string
}
type step struct {
	Op         string
	Path       []string
	Selections [][]string
	Raw, Cut   string
	Kind       string
	UseView    bool
}
type testCase struct {
	ID, Kind, Fault string
	Limits          map[string]int
	Steps           []step
	Relay           bool
	Mount           bool
}
type input struct {
	Nodes []nodeSpec
	Cases []testCase
}

func create(specs []nodeSpec, t testCase, own func(string) wire.Wire, checks *[][2]any) (declaration, map[string]declaration, error) {
	definitions := map[string]nodeSpec{}
	for _, s := range specs {
		if _, found := definitions[s.ID]; found {
			return declaration{}, nil, fmt.Errorf("duplicate node")
		}
		definitions[s.ID] = s
	}
	if t.Fault == "cycle" {
		spec := definitions["root"]
		spec.Children = append(spec.Children, [2]string{"loop", "root"})
		definitions["root"] = spec
	}
	gates := map[string]*policy{}
	for id, limit := range t.Limits {
		gates[id] = &policy{id, limit, checks}
	}
	nodes := map[string]declaration{}
	visiting := map[string]bool{}
	var build func(string) (declaration, error)
	build = func(id string) (declaration, error) {
		if visiting[id] {
			return declaration{}, fmt.Errorf("cyclic declaration")
		}
		if n, ok := nodes[id]; ok {
			return n, nil
		}
		s, ok := definitions[id]
		if !ok {
			return declaration{}, fmt.Errorf("missing declaration")
		}
		visiting[id] = true
		entries := []entry{}
		for _, e := range s.Children {
			child, err := build(e[1])
			if err != nil {
				return declaration{}, err
			}
			entries = append(entries, entry{Key: e[0], Node: child})
		}
		if id == "root" && t.Fault == "duplicate" {
			entries = append(entries, entries[0])
		}
		if id == "root" && t.Fault == "invalidKey" {
			entries = append(entries, entry{Key: string([]byte{0xff}), Node: nodes["leaf"]})
		}
		origin := refuse
		if s.Own != nil {
			origin = own(*s.Own)
		}
		if s.Policy != "" && gates[s.Policy] == nil {
			return declaration{}, fmt.Errorf("missing policy")
		}
		n, err := compose(origin, gates[s.Policy], entries)
		if err != nil {
			return declaration{}, err
		}
		delete(visiting, id)
		nodes[id] = n
		return n, nil
	}
	root, err := build("root")
	return root, nodes, err
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
	count   int
}

func borrowed(sender wire.Wire, deliveries <-chan delivery) bool {
	if sender.Send([]string{"borrowed"}, event("borrowed")) != nil {
		return false
	}
	got := wait(deliveries)
	return reflect.DeepEqual(got.path, []string{"borrowed"}) && string(got.message.Frame.Data) == `"borrowed"`
}
func observe(specs []nodeSpec, t testCase) any {
	s := &scope{}
	defer s.close()
	source, receiver := s.pair()
	var sender wire.Wire = source
	var mounted wire.Endpoint
	if t.Mount {
		mounted = duplex.Mount(map[string]wire.Endpoint{"mounted": source})
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
	deliveries := make(chan delivery, 32)
	counts := map[string]int{}
	off, err := receiver.Receive(wire.Receiver{Message: func(p []string, m wire.Message) {
		if len(p) != 1 {
			panic("unexpected destination path")
		}
		counts[p[0]]++
		deliveries <- delivery{append([]string{}, p...), m, counts[p[0]]}
	}})
	check(err)
	s.cleanups = append(s.cleanups, off)
	checks := [][2]any{}
	unchanged := true
	var expected wire.Message
	marker := &struct{}{}
	contexts := map[*wire.ReturnAddress]*struct{}{}
	origins := map[string]wire.Wire{}
	own := func(name string) wire.Wire {
		if w := origins[name]; w != nil {
			return w
		}
		w := &access{func(p []string, m wire.Message) error {
			unchanged = unchanged && len(p) == 0 && reflect.DeepEqual(m.Frame, expected.Frame) && m.Return == expected.Return && contexts[m.Return] == marker
			return duplex.At(sender, []string{name}).Send(p, m)
		}}
		origins[name] = w
		return w
	}
	root, nodes, err := create(specs, t, own, &checks)
	if err != nil {
		return map[string]any{"construction": "refused"}
	}
	outcomes := []string{}
	observed := [][2]any{}
	partsRetained := retained(root)
	var selected wire.Wire
	for index, action := range t.Steps {
		switch action.Op {
		case "captureView":
			selected = bind(root)
			for _, prefix := range action.Selections {
				selected = duplex.At(selected, prefix)
			}
		case "rebind":
			origin, gate, children := parts(root)
			for j, e := range children {
				if e.Key == "a" {
					children[j].Node = mustBuild(ownOf(e.Node), gateOf(e.Node), []entry{{Key: "b", Node: mustBuild(own("replacement"), nil, nil)}})
				}
			}
			root = mustBuild(origin, gate, children)
		case "rebuild":
			root = rebuild(root, action.Cut == "all", map[declaration]declaration{})
			partsRetained = partsRetained && retained(root)
		case "substitute":
			origin, gate, children := parts(root)
			for j, e := range children {
				if e.Key == "a" {
					children[j].Node = rebuild(e.Node, true, map[declaration]declaration{})
				}
			}
			root = mustBuild(origin, gate, children)
		case "boundChild":
			selected := duplex.At(bind(root), []string{"a", "b"})
			root = mustBuild(ownOf(root), gateOf(root), []entry{{Key: "a", Node: mustBuild(selected, nil, nil)}})
		case "resetPolicy":
			origin, _, children := parts(root)
			root = mustBuild(origin, &policy{"outer", t.Limits["outer"], &checks}, children)
		case "sharePolicy":
			origin, gate, children := parts(root)
			for j, e := range children {
				if e.Key == "a" {
					o, _, c := parts(e.Node)
					children[j].Node = mustBuild(o, gate, c)
				}
			}
			root = mustBuild(origin, gate, children)
		case "dropOwn":
			_, gate, children := parts(root)
			root = mustBuild(refuse, gate, children)
		case "omit", "rename", "extra":
			origin, gate, children := parts(root)
			next := []entry{}
			for _, e := range children {
				if e.Key == "a" && action.Op == "omit" {
					continue
				}
				if e.Key == "a" && action.Op == "rename" {
					e.Key = "renamed"
				}
				next = append(next, e)
			}
			if action.Op == "extra" {
				next = append(next, entry{Key: "extra", Node: nodes["leaf"]})
			}
			root = mustBuild(origin, gate, next)
		case "send", "invalidPath", "control":
			target := root
			if action.Raw != "" {
				target = nodes[action.Raw]
			}
			w := bind(target)
			if action.UseView {
				if selected == nil {
					panic("no captured view")
				}
				w = selected
			}
			for _, prefix := range action.Selections {
				w = duplex.At(w, prefix)
			}
			expected = event(fmt.Sprintf("%s:%d", t.ID, index))
			expected.Return = &wire.ReturnAddress{Wire: refuse}
			contexts[expected.Return] = marker
			if action.Op == "control" {
				expected.Frame = wire.ProfileFrame{Version: 1, Kind: wire.ProfileKind(action.Kind), ID: "c:1"}
				if action.Kind == "response" {
					expected.Frame.Result = json.RawMessage("null")
				}
			}
			path := action.Path
			if action.Op == "invalidPath" {
				path = []string{string([]byte{0xff})}
			}
			err := w.Send(path, expected)
			if err != nil {
				outcomes = append(outcomes, "refused")
			} else {
				outcomes = append(outcomes, "admitted")
				got := wait(deliveries)
				if string(got.message.Frame.Data) != string(expected.Frame.Data) {
					panic("message changed or unexpected delivery")
				}
				observed = append(observed, [2]any{got.path[0], got.count})
			}
		default:
			panic("unknown step: " + action.Op)
		}
	}
	_, endpoint := bind(root).(wire.Endpoint)
	root = declaration{}
	if mounted != nil {
		check(mounted.Close(duplex.CodeNormal, "released"))
	}
	return map[string]any{"outcomes": outcomes, "checks": checks, "deliveries": observed, "unchanged": unchanged, "sendOnly": !endpoint, "borrowedUsable": borrowed(source, deliveries), "partsRetained": partsRetained}
}

func pending(specs []nodeSpec, t testCase) any {
	s := &scope{}
	defer s.close()
	sender, receiver := s.pair()
	captured := make(chan wire.Message, 1)
	cancelled := make(chan string, 1)
	deliveries := make(chan delivery, 4)
	replies := make(chan string, 1)
	off, err := receiver.Receive(wire.Receiver{Message: func(p []string, m wire.Message) {
		if m.Frame.Kind == wire.ProfileEvent {
			deliveries <- delivery{p, m, 1}
			return
		}
		lifecycle := m.Return.Wire
		check(lifecycle.Send([]string{"invocation.capture", "old"}, wire.Message{Frame: wire.ProfileFrame{Version: 1, Kind: wire.ProfileEvent, Data: json.RawMessage("null")}, Return: &wire.ReturnAddress{Wire: &access{func(p []string, m wire.Message) error {
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
	checks := [][2]any{}
	preserved := true
	original := &wire.ReturnAddress{Wire: &access{func(p []string, m wire.Message) error {
		if len(p) != 0 || m.Frame.Kind != wire.ProfileResponse || m.Frame.Error != nil {
			panic("unexpected reply")
		}
		var result string
		check(json.Unmarshal(m.Frame.Result, &result))
		replies <- result
		return nil
	}}}
	root, _, err := create(specs, t, func(name string) wire.Wire {
		return &access{func(p []string, m wire.Message) error {
			preserved = preserved && m.Return == original && len(p) == 0
			return duplex.At(sender, []string{name}).Send(p, m)
		}}
	}, &checks)
	check(err)
	check(duplex.At(duplex.At(bind(root), []string{"a"}), []string{"b"}).Send(nil, wire.Message{Frame: wire.ProfileFrame{Version: 1, Kind: wire.ProfileRequest, ID: "c:1", Params: json.RawMessage("null")}, Return: original}))
	old := wait(captured)
	root = rebuild(root, true, map[declaration]declaration{})
	// Replace the tree's aliases after admission; the captured invocation stays old.
	own, gate, _ := parts(root)
	replacement := mustBuild(duplex.At(sender, []string{"new"}), nil, nil)
	root = mustBuild(own, gate, []entry{{Key: "alias", Node: replacement}, {Key: "a", Node: replacement}})
	newRefused := bind(root).Send([]string{"alias"}, event("new")) != nil
	check(old.Return.Wire.Send(nil, wire.Message{Frame: wire.ProfileFrame{Version: 1, Kind: wire.ProfileResponse, ID: old.Frame.ID, Result: json.RawMessage(`"old"`)}}))
	late := wait(replies)
	check(old.Return.Wire.Send([]string{"invocation.control"}, wire.Message{Frame: wire.ProfileFrame{Version: 1, Kind: wire.ProfileCancel, ID: old.Frame.ID}}))
	controls := []string{wait(cancelled)}
	check(old.Return.Wire.Send([]string{"invocation.release", "old"}, event("release")))
	check(old.Return.Wire.Send([]string{"invocation.done", "old"}, event("done")))
	return map[string]any{"checks": checks, "newCallRefused": newRefused, "lateReply": late, "cancelled": controls, "returnPreserved": preserved, "borrowedUsable": borrowed(sender, deliveries)}
}
func main() {
	if os.Getenv("NIGHTSEAM_BITWIRE_CARRIER") != "local" && os.Getenv("NIGHTSEAM_BITWIRE_CARRIER") != "peer" {
		panic("expected local or peer carrier")
	}
	data, err := os.ReadFile(os.Args[1])
	check(err)
	var fixture input
	check(json.Unmarshal(data, &fixture))
	output := []any{}
	for _, t := range fixture.Cases {
		var observations any
		if t.Kind == "pending" {
			observations = pending(fixture.Nodes, t)
		} else {
			observations = observe(fixture.Nodes, t)
		}
		output = append(output, map[string]any{"id": t.ID, "observations": observations})
	}
	check(json.NewEncoder(os.Stdout).Encode(output))
}
