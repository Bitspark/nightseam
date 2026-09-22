package conformance

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// A scenario opens its connections with the runner's own ops, written
// "on": "runner", and the runner expands each into the testees' ops from
// what the two answered hello with: whichever side can listen does, so a
// language whose runtime only dials is still held in every role. The
// profile's role — server, client — is the scenario's to assign; who opens
// the socket beneath is the runner's to choose.
//
//	pair.conns          two raw connections, one each side
//	pair.peers          a peer of each role: server on one side, client on the other
//	pair.peer_and_conn  a peer of a role on one side, a raw connection on the other
//
// Where the natural side cannot listen and the other can, both take what
// they need over a raw connection the other side accepted, through
// peer.over; where neither can, the scenario is skipped for that pairing.

// needsOf is what a step asks of the side that runs it: the layer its op
// belongs to, and the features its op or arguments use. A runner step asks
// of both sides what its expansion will, less what the runner chooses —
// who listens, and the lazy consumption the peer.over path needs — which
// the expanded steps say for themselves.
func needsOf(step Step) map[string][]string {
	needs := map[string][]string{}
	add := func(side, need string) { needs[side] = append(needs[side], need) }
	family, _, _ := strings.Cut(step.Op, ".")
	if family == "pair" {
		a, b := "a", "b"
		switch step.Op {
		case "pair.conns":
			add(a, "seam")
			add(b, "seam")
		case "pair.peers":
			add(a, "peer")
			add(b, "peer")
			server, _ := step.Args["server"].(string)
			client := other(server)
			addOptionNeeds(add, server, step.Args["server_options"])
			addOptionNeeds(add, client, step.Args["client_options"])
		case "pair.peer_and_conn":
			peer, _ := step.Args["peer"].(string)
			add(peer, "peer")
			add(other(peer), "seam")
			addOptionNeeds(add, peer, step.Args["options"])
		}
		return needs
	}
	side := step.On
	if layer, ok := layerOfFamily[family]; ok {
		add(side, layer)
	}
	switch step.Op {
	case "conn.listen", "peer.listen":
		add(side, "listen")
	case "conn.pipe":
		add(side, "pipe")
	case "peer.observed":
		add(side, "observer")
	}
	if consume, _ := step.Args["consume"].(string); consume == "lazy" {
		add(side, "lazy")
	}
	addOptionNeeds(add, side, step.Args["options"])
	carrier, _ := step.Args["carrier"].(string)
	for _, need := range runtimesOfGenerated(step.Op, carrier) {
		add(side, need)
	}
	return needs
}

// runtimesOfGenerated is what a generated op asks of its testee beyond the
// generated layer: the runtimes the rendered packages and the Wire fixture
// link. Every op that converts callables — the live tier's, the
// combinators', the owners', publication, forwarding, and the whole Wire
// construction, whose Cell has callable slots — needs live; a Wire op that
// carries the Cell needs the tunnel where its carrier is a prepared
// channel. carrier is the op's carrier argument, resolved through the row
// a foreach bound; one still unresolved is taken as possibly a channel, so
// that a need is never understated.
func runtimesOfGenerated(op, carrier string) []string {
	switch {
	case strings.HasPrefix(op, "gen.live_"), strings.HasPrefix(op, "client.live_"),
		strings.HasPrefix(op, "gen.combinator_"), strings.HasPrefix(op, "client.combinator_"),
		strings.HasPrefix(op, "gen.owners_"), strings.HasPrefix(op, "client.owners_"),
		strings.HasPrefix(op, "gen.publication_"), strings.HasPrefix(op, "gen.forwarding_"):
		return []string{"live"}
	case strings.HasPrefix(op, "gen.wire_"):
		if carrier == "channel" || strings.HasPrefix(carrier, "$") {
			return []string{"live", "tunnel"}
		}
		return []string{"live"}
	}
	return nil
}

// withRow is the steps with the row a foreach bound applied to their
// arguments — `$row.carrier` read as the row's carrier — so that what a
// step asks of its side is derived from what it will send: at load over
// every row, at run for the one the scenario carries. Only a string
// argument that names a member of the row is replaced; a handle an earlier
// step bound stays as written, since it says nothing about needs.
func withRow(steps []Step, as string, row map[string]any) []Step {
	if row == nil || as == "" {
		return steps
	}
	prefix := "$" + as + "."
	out := make([]Step, len(steps))
	for i, step := range steps {
		out[i] = step
		if len(step.Args) == 0 {
			continue
		}
		args := make(map[string]any, len(step.Args))
		for name, value := range step.Args {
			if s, ok := value.(string); ok && strings.HasPrefix(s, prefix) {
				if member, found := row[strings.TrimPrefix(s, prefix)]; found {
					value = member
				}
			}
			args[name] = value
		}
		out[i].Args = args
	}
	return out
}

// addOptionNeeds reads a peer's options for the features they ask for.
func addOptionNeeds(add func(side, need string), side string, options any) {
	object, _ := options.(map[string]any)
	if object == nil {
		return
	}
	if on, _ := object["propagate"].(bool); on {
		add(side, "propagator")
	}
	if on, _ := object["observe"].(bool); on {
		add(side, "observer")
	}
}

var layerOfFamily = map[string]string{
	"conn": "seam", "peer": "peer", "call": "peer", "tunnel": "tunnel",
	"live": "live",
	"gen":  "generated", "client": "generated", "server": "generated",
}

func other(side string) string {
	if side == "a" {
		return "b"
	}
	return "a"
}

// SideNeeds is what each side's testee must answer hello with to run the
// steps: their needs by side, each sorted and without repeats.
func SideNeeds(steps []Step) map[string][]string {
	out := map[string][]string{}
	for _, step := range steps {
		for side, needs := range needsOf(step) {
			out[side] = append(out[side], needs...)
		}
	}
	for side, needs := range out {
		sort.Strings(needs)
		out[side] = dedupe(needs)
	}
	return out
}

// union is every need of either side, sorted: what a scenario declares.
func union(sides map[string][]string) []string {
	var all []string
	for _, needs := range sides {
		all = append(all, needs...)
	}
	sort.Strings(all)
	return dedupe(all)
}

// holdDeclared holds a scenario's declared needs to the union of what its
// steps ask over every expansion — each row of a foreach applied, since the
// carrier a row selects is a need that row brings — so the file says what
// it needs and no more.
func holdDeclared(declared []string, expansions []Scenario) error {
	sorted := append([]string(nil), declared...)
	sort.Strings(sorted)
	var asked []string
	for _, s := range expansions {
		asked = append(asked, union(SideNeeds(withRow(s.Steps, s.RowAs, s.Row)))...)
	}
	sort.Strings(asked)
	asked = dedupe(asked)
	if strings.Join(sorted, " ") == strings.Join(asked, " ") {
		return nil
	}
	return fmt.Errorf("declares needs [%s] but its steps ask for [%s]", strings.Join(sorted, ", "), strings.Join(asked, ", "))
}

// checkRunnerStep holds a runner step to its shape at load: the op is a
// pair op, the sides it names are a or b, and it binds by name.
func checkRunnerStep(step Step) error {
	if step.On != "runner" {
		if strings.HasPrefix(step.Op, "pair.") {
			return fmt.Errorf("%s is the runner's op and is written on runner", step.Op)
		}
		return nil
	}
	sideArg := func(name string) error {
		v, ok := step.Args[name].(string)
		if !ok || (v != "a" && v != "b") {
			return fmt.Errorf("%s names %s, a or b", step.Op, name)
		}
		return nil
	}
	binds, _ := step.Bind.(map[string]any)
	if step.Bind != nil && binds == nil {
		return fmt.Errorf("%s binds by name: an object", step.Op)
	}
	switch step.Op {
	case "pair.conns":
	case "pair.peers":
		if err := sideArg("server"); err != nil {
			return err
		}
	case "pair.peer_and_conn":
		if err := sideArg("peer"); err != nil {
			return err
		}
		if role, _ := step.Args["role"].(string); role != "client" && role != "server" {
			return fmt.Errorf("%s names a role, client or server", step.Op)
		}
	default:
		return fmt.Errorf("%s is not an op of the runner", step.Op)
	}
	if step.HasExpect || step.ExpectError != nil || step.Assert != nil || step.Repeat != nil {
		return fmt.Errorf("%s holds nothing of its own; expect what the connection carries", step.Op)
	}
	return nil
}

// mirrorRunnerStep exchanges the sides a runner step names.
func mirrorRunnerStep(step Step) Step {
	m := step
	m.Args = map[string]any{}
	for key, value := range step.Args {
		switch {
		case key == "server" || key == "peer":
			m.Args[key] = other(value.(string))
		case strings.HasSuffix(key, "_a"):
			m.Args[strings.TrimSuffix(key, "_a")+"_b"] = value
		case strings.HasSuffix(key, "_b"):
			m.Args[strings.TrimSuffix(key, "_b")+"_a"] = value
		default:
			m.Args[key] = value
		}
	}
	if binds, ok := step.Bind.(map[string]any); ok && step.Op == "pair.conns" {
		flipped := map[string]any{}
		for key, value := range binds {
			flipped[other(key)] = value
		}
		m.Bind = flipped
	}
	return m
}

// expand replaces every runner step by the testees' ops, given what each
// answered hello with, or says why the pairing cannot run the scenario.
func expand(s Scenario, hello map[string]Hello) ([]Step, string) {
	var out []Step
	n := 0
	for _, step := range s.Steps {
		if step.On != "runner" {
			out = append(out, step)
			continue
		}
		n++
		expanded, skip := expandPair(step, hello, fmt.Sprintf("_p%d", n))
		if skip != "" {
			return nil, skip
		}
		out = append(out, expanded...)
	}
	return out, ""
}

func expandPair(step Step, hello map[string]Hello, prefix string) ([]Step, string) {
	can := func(side, feature string) bool { return hello[side].Has(feature) }
	bindOf := func(key string) any {
		binds, _ := step.Bind.(map[string]any)
		if name, ok := binds[key].(string); ok {
			return name
		}
		return nil
	}
	arg := func(name string) any { return step.Args[name] }
	number := func(v any) json.Number {
		if n, ok := v.(json.Number); ok {
			return n
		}
		return ""
	}
	// A raw connection each way: listener listens with its own limit, the
	// dialer dials with its own; each is consumed as its side asked.
	rawPair := func(listener string, limitL, limitD, consumeL, consumeD any) (steps []Step, url, accepted, dialed string) {
		dialer := other(listener)
		url, listenerHandle := prefix+"_url", prefix+"_l"
		accepted, dialed = prefix+"_c"+listener, prefix+"_c"+dialer
		listenArgs := map[string]any{}
		if n := number(limitL); n != "" {
			listenArgs["limit"] = n
		}
		dialArgs := map[string]any{"url": "$" + url}
		if n := number(limitD); n != "" {
			dialArgs["limit"] = n
		}
		if c, ok := consumeD.(string); ok && c != "" {
			dialArgs["consume"] = c
		}
		acceptArgs := map[string]any{"on": "$" + listenerHandle}
		if c, ok := consumeL.(string); ok && c != "" {
			acceptArgs["consume"] = c
		}
		steps = []Step{
			{On: listener, Op: "conn.listen", Args: listenArgs, Bind: map[string]any{"handle": listenerHandle, "url": url}},
			{On: dialer, Op: "conn.dial", Args: dialArgs, Bind: dialed},
			{On: listener, Op: "conn.accept", Args: acceptArgs, Bind: accepted},
		}
		return steps, url, accepted, dialed
	}
	// A peer over a raw connection this side holds, lazily consumed since
	// the peer reads it itself.
	over := func(side, conn, role string, options any, bind any) Step {
		args := map[string]any{"on": "$" + conn, "role": role}
		if options != nil {
			args["options"] = options
		}
		return Step{On: side, Op: "peer.over", Args: args, Bind: bind}
	}
	limitOf := func(options any) any {
		object, _ := options.(map[string]any)
		if object == nil {
			return nil
		}
		return object["max_frame_bytes"]
	}
	switch step.Op {
	case "pair.conns":
		listener := ""
		for _, side := range []string{"a", "b"} {
			if can(side, "listen") {
				listener = side
				break
			}
		}
		if listener == "" {
			return nil, "neither testee can listen"
		}
		dialer := other(listener)
		steps, _, accepted, dialed := rawPair(listener, arg("limit_"+listener), arg("limit_"+dialer), arg("consume_"+listener), arg("consume_"+dialer))
		// The scenario's names for each side's connection.
		steps[2].Bind = bindOrInternal(bindOf(listener), accepted)
		steps[1].Bind = bindOrInternal(bindOf(dialer), dialed)
		return steps, ""
	case "pair.peers":
		server, _ := arg("server").(string)
		client := other(server)
		serverOptions, clientOptions := arg("server_options"), arg("client_options")
		if can(server, "listen") {
			listenArgs := map[string]any{}
			if serverOptions != nil {
				listenArgs["options"] = serverOptions
			}
			dialArgs := map[string]any{"url": "$" + prefix + "_url"}
			if clientOptions != nil {
				dialArgs["options"] = clientOptions
			}
			return []Step{
				{On: server, Op: "peer.listen", Args: listenArgs, Bind: map[string]any{"handle": prefix + "_l", "url": prefix + "_url"}},
				{On: client, Op: "peer.dial", Args: dialArgs, Bind: bindOrInternal(bindOf("client"), prefix+"_pc")},
				{On: server, Op: "peer.accept", Args: map[string]any{"on": "$" + prefix + "_l"}, Bind: bindOrInternal(bindOf("server"), prefix+"_ps")},
			}, ""
		}
		if !can(client, "listen") {
			return nil, "neither testee can listen"
		}
		if !can(server, "lazy") || !can(client, "lazy") {
			return nil, fmt.Sprintf("the %s side cannot listen, and a peer over a connection the other side accepted needs lazy consumption on both", server)
		}
		// The client side listens a raw connection; each side takes a peer of
		// its role over its end.
		steps, _, accepted, dialed := rawPair(client, limitOf(clientOptions), limitOf(serverOptions), "lazy", "lazy")
		steps = append(steps,
			over(client, accepted, "client", clientOptions, bindOrInternal(bindOf("client"), prefix+"_pc")),
			over(server, dialed, "server", serverOptions, bindOrInternal(bindOf("server"), prefix+"_ps")),
		)
		return steps, ""
	case "pair.peer_and_conn":
		peer, _ := arg("peer").(string)
		raw := other(peer)
		role, _ := arg("role").(string)
		options := arg("options")
		if can(peer, "listen") {
			listenArgs := map[string]any{}
			if options != nil {
				listenArgs["options"] = options
			}
			dialArgs := map[string]any{"url": "$" + prefix + "_url"}
			if n := number(arg("limit")); n != "" {
				dialArgs["limit"] = n
			}
			if c, ok := arg("consume").(string); ok && c != "" {
				dialArgs["consume"] = c
			}
			if role == "server" {
				return []Step{
					{On: peer, Op: "peer.listen", Args: listenArgs, Bind: map[string]any{"handle": prefix + "_l", "url": prefix + "_url"}},
					{On: raw, Op: "conn.dial", Args: dialArgs, Bind: bindOrInternal(bindOf("conn"), prefix+"_c")},
					{On: peer, Op: "peer.accept", Args: map[string]any{"on": "$" + prefix + "_l"}, Bind: bindOrInternal(bindOf("peer"), prefix+"_p")},
				}, ""
			}
			// A client-role peer over a socket this side accepted.
			if !can(peer, "lazy") {
				return nil, fmt.Sprintf("the %s side takes a client peer over a connection it accepted, which needs lazy consumption", peer)
			}
			steps, _, accepted, dialed := rawPair(peer, limitOf(options), arg("limit"), "lazy", arg("consume"))
			steps[1].Bind = bindOrInternal(bindOf("conn"), dialed)
			steps = append(steps, over(peer, accepted, role, options, bindOrInternal(bindOf("peer"), prefix+"_p")))
			return steps, ""
		}
		if !can(raw, "listen") {
			return nil, "neither testee can listen"
		}
		if !can(peer, "lazy") {
			return nil, fmt.Sprintf("the %s side cannot listen, and a peer over a connection it dialed needs lazy consumption", peer)
		}
		steps, _, accepted, dialed := rawPair(raw, arg("limit"), limitOf(options), arg("consume"), "lazy")
		steps[2].Bind = bindOrInternal(bindOf("conn"), accepted)
		steps = append(steps, over(peer, dialed, role, options, bindOrInternal(bindOf("peer"), prefix+"_p")))
		return steps, ""
	}
	return nil, "not a runner op: " + step.Op
}

// bindOrInternal is the scenario's name for a handle, or the runner's own
// when the scenario gave none.
func bindOrInternal(name any, internal string) any {
	if name != nil {
		return name
	}
	return internal
}
