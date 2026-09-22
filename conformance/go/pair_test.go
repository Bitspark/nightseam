package conformance

import (
	"strings"
	"testing"
)

func hellos(listenA, listenB bool) map[string]Hello {
	features := func(listen bool) []string {
		f := []string{"pipe", "observer", "propagator", "lazy"}
		if listen {
			f = append(f, "listen")
		}
		return f
	}
	layers := []string{"seam", "peer", "tunnel"}
	return map[string]Hello{"a": {Layers: layers, Features: features(listenA)}, "b": {Layers: layers, Features: features(listenB)}}
}

func ops(steps []Step) string {
	var out []string
	for _, s := range steps {
		out = append(out, s.On+":"+s.Op)
	}
	return strings.Join(out, " ")
}

// TestAPairOfPeersIsOpenedByWhoeverCanListen: the server side listens when
// it can; else the client side listens a raw connection and each side takes
// a peer of its role over its end; neither able, the pairing is skipped.
func TestAPairOfPeersIsOpenedByWhoeverCanListen(t *testing.T) {
	pair := Step{On: "runner", Op: "pair.peers", Args: map[string]any{"server": "a", "server_options": map[string]any{"observe": true}}, Bind: map[string]any{"server": "pa", "client": "pb"}}
	s := Scenario{Steps: []Step{pair}}

	steps, skip := expand(s, hellos(true, true))
	if skip != "" || ops(steps) != "a:peer.listen b:peer.dial a:peer.accept" {
		t.Fatalf("both listen: %q %s", skip, ops(steps))
	}
	if steps[2].Bind != "pa" || steps[1].Bind != "pb" {
		t.Fatalf("the scenario's names were not bound: %v %v", steps[2].Bind, steps[1].Bind)
	}
	if steps[0].Args["options"] == nil {
		t.Fatal("the server's options did not reach peer.listen")
	}

	steps, skip = expand(s, hellos(false, true))
	if skip != "" || ops(steps) != "b:conn.listen a:conn.dial b:conn.accept b:peer.over a:peer.over" {
		t.Fatalf("server cannot listen: %q %s", skip, ops(steps))
	}
	if steps[4].Args["role"] != "server" || steps[3].Args["role"] != "client" {
		t.Fatalf("the roles were not kept apart from who listened: %v %v", steps[4].Args, steps[3].Args)
	}
	if steps[4].Bind != "pa" || steps[3].Bind != "pb" || steps[4].Args["options"] == nil {
		t.Fatalf("the scenario's names or options were lost over the raw path: %v %v", steps[4], steps[3])
	}
	if steps[1].Args["consume"] != "lazy" || steps[2].Args["consume"] != "lazy" {
		t.Fatal("a peer over a raw connection takes it lazily")
	}

	if _, skip = expand(s, hellos(false, false)); !strings.Contains(skip, "neither") {
		t.Fatalf("neither listens: %q", skip)
	}
}

// TestAPairOfConnectionsAndAPeerWithAConn: the other two runner ops,
// their natural and their fallback paths.
func TestAPairOfConnectionsAndAPeerWithAConn(t *testing.T) {
	conns := Scenario{Steps: []Step{{On: "runner", Op: "pair.conns", Args: map[string]any{"limit_a": number("1024")}, Bind: map[string]any{"a": "ca", "b": "cb"}}}}
	steps, skip := expand(conns, hellos(false, true))
	if skip != "" || ops(steps) != "b:conn.listen a:conn.dial b:conn.accept" {
		t.Fatalf("conns, b listens: %q %s", skip, ops(steps))
	}
	if steps[1].Args["limit"] != number("1024") || steps[1].Bind != "ca" || steps[2].Bind != "cb" {
		t.Fatalf("a's limit and the names did not follow a to the dialing side: %v %v", steps[1], steps[2])
	}

	mixed := Scenario{Steps: []Step{{On: "runner", Op: "pair.peer_and_conn", Args: map[string]any{"peer": "b", "role": "server", "options": map[string]any{"propagate": true}}, Bind: map[string]any{"peer": "pb", "conn": "ca"}}}}
	steps, skip = expand(mixed, hellos(true, true))
	if skip != "" || ops(steps) != "b:peer.listen a:conn.dial b:peer.accept" {
		t.Fatalf("mixed, peer listens: %q %s", skip, ops(steps))
	}
	steps, skip = expand(mixed, hellos(true, false))
	if skip != "" || ops(steps) != "a:conn.listen b:conn.dial a:conn.accept b:peer.over" {
		t.Fatalf("mixed, raw side listens: %q %s", skip, ops(steps))
	}
	if steps[3].Args["role"] != "server" || steps[3].Bind != "pb" || steps[2].Bind != "ca" {
		t.Fatalf("the peer's role or the names were lost: %v %v", steps[3], steps[2])
	}
}

// TestNeedsAreHeldPerSide: what each side's steps ask is derived from them,
// a runner step asks of both sides what it will expand to less what the
// runner chooses, and a declaration that differs from the union is refused.
func TestNeedsAreHeldPerSide(t *testing.T) {
	steps := []Step{
		{On: "runner", Op: "pair.peer_and_conn", Args: map[string]any{"peer": "b", "role": "server", "options": map[string]any{"propagate": true}}},
		{On: "a", Op: "conn.send", Args: map[string]any{"on": "$ca"}},
		{On: "b", Op: "peer.observed", Args: map[string]any{"on": "$pb"}},
		{On: "a", Op: "conn.pipe", Args: map[string]any{"consume": "lazy"}},
	}
	sides := SideNeeds(steps)
	if got := strings.Join(sides["a"], " "); got != "lazy pipe seam" {
		t.Fatalf("a needs %q", got)
	}
	if got := strings.Join(sides["b"], " "); got != "observer peer propagator" {
		t.Fatalf("b needs %q", got)
	}
	if err := holdDeclared([]string{"lazy", "observer", "peer", "pipe", "propagator", "seam"}, []Scenario{{Steps: steps}}); err != nil {
		t.Fatal(err)
	}
	if err := holdDeclared([]string{"listen", "peer", "seam"}, []Scenario{{Steps: steps}}); err == nil || !strings.Contains(err.Error(), "declares needs") {
		t.Fatalf("a declaration differing from the steps was not refused: %v", err)
	}
	// A generated op asks for the runtimes it runs over, and a carrier a row
	// selects is read through the row: a channel brings the tunnel, a socket
	// does not, and a carrier no row resolves is taken as possibly a channel.
	carried := []Step{{On: "a", Op: "gen.wire_serve", Args: map[string]any{"carrier": "$row.carrier"}}, {On: "b", Op: "gen.live_dial", Args: map[string]any{"url": "$origin"}}}
	if got := strings.Join(union(SideNeeds(withRow(carried, "row", map[string]any{"carrier": "channel"}))), " "); got != "generated live tunnel" {
		t.Fatalf("a channel row asks %q", got)
	}
	if got := strings.Join(union(SideNeeds(withRow(carried, "row", map[string]any{"carrier": "socket"}))), " "); got != "generated live" {
		t.Fatalf("a socket row asks %q", got)
	}
	if got := strings.Join(union(SideNeeds(carried)), " "); got != "generated live tunnel" {
		t.Fatalf("an unresolved carrier asks %q", got)
	}
	if err := checkRunnerStep(Step{On: "a", Op: "pair.conns"}); err == nil {
		t.Fatal("a pair op on a side was not refused")
	}
	if err := checkRunnerStep(Step{On: "runner", Op: "pair.peers", Args: map[string]any{"server": "c"}}); err == nil {
		t.Fatal("a server that is neither side was not refused")
	}
}

// TestARunnerStepMirrors: exchanging the sides exchanges what a runner
// step names of them.
func TestARunnerStepMirrors(t *testing.T) {
	m := mirrorRunnerStep(Step{On: "runner", Op: "pair.peers", Args: map[string]any{"server": "a", "server_options": 1}})
	if m.Args["server"] != "b" || m.Args["server_options"] != 1 {
		t.Fatalf("pair.peers mirrored to %v", m.Args)
	}
	m = mirrorRunnerStep(Step{On: "runner", Op: "pair.conns", Args: map[string]any{"limit_a": 1, "consume_b": "lazy"}, Bind: map[string]any{"a": "ca", "b": "cb"}})
	if m.Args["limit_b"] != 1 || m.Args["consume_a"] != "lazy" || m.Bind.(map[string]any)["b"] != "ca" {
		t.Fatalf("pair.conns mirrored to %v %v", m.Args, m.Bind)
	}
}

func number(s string) any {
	v, _ := decode([]byte(s))
	return v
}
