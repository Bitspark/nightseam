package session_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	duplexruntime "github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/session/go"
	"github.com/Bitspark/nightseam/session/go/sessiontest"
	"github.com/Bitspark/nightseam/tunnel/go"
)

// The frames the gate speaks, in the one spelling both languages hold each
// other to. A relay forwards a frame verbatim and mints the ids it sends on
// the same way in either language, so every frame here is held byte for byte
// on the other side of the boundary: session/ts/src/interop.ts carries these
// same literals.
const (
	// The machine's first word, which is also the gate's go-ahead: the
	// consumers are attached and the first of them holds control.
	gateReady = `{"version":1,"kind":"event","event":"changed","tracestate":"nightseam=gate","data":{"text":"ready","count":0}}`
	// A consumer's deciding request, under an id of its own and carrying
	// members no relay knows, and the same request as the machine sees it:
	// the session's own id, and every other member where it was.
	gateEcho   = `{"version":1,"kind":"request","id":"c:7","traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01","method":"echo","params":{"text":"gate","count":1},"tracestate":"nightseam=gate","baggage":{"tenant":"acme"}}`
	gateMinted = `{"version":1,"kind":"request","id":"c:1","traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01","method":"echo","params":{"text":"gate","count":1},"tracestate":"nightseam=gate","baggage":{"tenant":"acme"}}`
	// The machine's answer, under the id the session asked with, and that
	// answer as the one consumer that asked reads it.
	gateAnswer   = `{"version":1,"kind":"response","id":"c:1","result":{"text":"etag","count":1}}`
	gateAnswered = `{"version":1,"kind":"response","id":"c:7","result":{"text":"etag","count":1}}`
	// What the machine asks of whoever holds control, and the holder saying
	// it has the ask — which is what moves control while the ask is open.
	gateAsk     = `{"version":1,"kind":"request","id":"s:1","traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b8-01","method":"reverse","params":{"text":"gate","count":1}}`
	gateNoticed = `{"version":1,"kind":"event","event":"noticed","data":{"text":"asked","count":1}}`
	// The answer of the consumer control left, which no machine sees, and the
	// answer of the one it moved to, which every machine does.
	gateStale = `{"version":1,"kind":"response","id":"s:1","result":{"text":"stale","count":1}}`
	gateHeld  = `{"version":1,"kind":"response","id":"s:1","result":{"text":"etag","count":1}}`
	// The one live frame after a replay, and the resuming consumer saying it
	// read the log and the frame that followed it.
	gateLive     = `{"version":1,"kind":"event","event":"changed","tracestate":"nightseam=late","data":{"text":"live","count":2}}`
	gateReplayed = `{"version":1,"kind":"event","event":"noticed","data":{"text":"replayed","count":6}}`
	// The close the machine's channel ends with, and which every attached
	// channel is ended with in turn.
	gateGone = "the machine went away"
)

// TestTypeScriptSessionInteroperability holds the two session components to
// each other over a real WebSocket, both ways: a session bound on the Go
// registry with the TypeScript driver's consumers attached, and a session
// bound on the TypeScript registry with this side's consumers attached. Each
// way runs the rules of the component across the boundary — a response to
// the one that asked, an event to every attached one, an ask to the holder
// and following a transfer while it is open, a replay from a sequence before
// anything live and in one order, the machine's close reaching every
// attached channel with its code and its reason, and the members neither
// relay knows travelling verbatim through both.
//
// The machine of each session speaks the registry's own language, over a
// pipe: the boundary this gate is about is the consumers'.
func TestTypeScriptSessionInteroperability(t *testing.T) {
	if testing.Short() {
		t.Skip("the TypeScript interoperability gate; skipped under -short")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node is required for the TypeScript interoperability gate; it is a gate, not a skip")
	}
	_, source, _, _ := runtime.Caller(0)
	driver := filepath.Join(filepath.Dir(source), "..", "ts", "src", "interop.ts")
	if _, err := os.Stat(driver); err != nil {
		t.Fatal(err)
	}
	governance := sessiontest.Probe(t)
	tunnels := make(chan *tunnel.Tunnel, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := duplexruntime.Accept(w, r, duplexruntime.ServerOptions{
			Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
			CheckOrigin:  func(*http.Request) bool { return true },
		})
		if err != nil {
			return
		}
		carrier, err := tunnel.New(peer, tunnel.Options{})
		if err != nil {
			return
		}
		tunnels <- carrier
		<-peer.Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	output := &transcript{}
	command := exec.CommandContext(ctx, node, "--experimental-strip-types", driver, "ws"+strings.TrimPrefix(server.URL, "http"))
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	// The driver ends when this side closes the connection; cancelling ends
	// it where the gate failed before that, and what it said is the other
	// half of any failure this side reports.
	defer func() { cancel(); _ = command.Wait() }()
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the TypeScript session driver said:\n%s", output)
		}
	})
	var carrier *tunnel.Tunnel
	select {
	case carrier = <-tunnels:
	case <-ctx.Done():
		t.Fatalf("the TypeScript session driver never connected: %s", output)
	}
	goRelay(ctx, t, carrier, governance)
	typeScriptRelay(ctx, t, carrier)
	// The driver waits for the connection to end, so that the closes it is
	// holding this side to are the session's own and not the transport's.
	carrier.Peer().Close()
	if err := command.Wait(); err != nil {
		t.Fatalf("the TypeScript session driver failed: %v\n%s", err, output)
	}
	if !strings.Contains(output.String(), `"ok":true`) {
		t.Fatalf("missing driver success: %s", output)
	}
}

// goRelay is the gate one way: the session is bound here and every consumer
// of it is the driver's, attached over a channel of the real connection in
// the order the driver opens them — the holder, the one control moves to,
// and the consumer that resumes from the sequence its own open carried.
func goRelay(ctx context.Context, t *testing.T, carrier *tunnel.Tunnel, governance session.Governance) {
	t.Helper()
	registry := session.New(session.Options{})
	up, machine := channels(t)
	if err := registry.Bind("go-relay", up, governance, session.NewMemoryLog(0)); err != nil {
		t.Fatal(err)
	}
	speaker := &end{t: t, name: "the machine", channel: machine}
	holder := attach(ctx, t, carrier, registry, "go-relay", session.Participant, "one")
	next := attach(ctx, t, carrier, registry, "go-relay", session.Participant, "two")
	if err := registry.Control("go-relay", holder); err != nil {
		t.Fatal(err)
	}
	speaker.send(gateReady)
	// A consumer's request reaches the machine under an id of the session's
	// own, with every member the relay knows nothing of where it was.
	speaker.takes(gateMinted)
	speaker.send(gateAnswer)
	// What the machine asks reaches the holder; the holder says so, and
	// control moves while the ask is open.
	speaker.send(gateAsk)
	speaker.takes(gateNoticed)
	if attention := registry.Attention(); len(attention) != 1 || attention[0] != "go-relay" {
		t.Fatalf("the session with an open ask is %v", attention)
	}
	if err := registry.Control("go-relay", next); err != nil {
		t.Fatal(err)
	}
	// Only the consumer control moved to answers it: the other's answer is
	// dropped, so the machine reads one answer and it is the holder's.
	speaker.takes(gateHeld)
	if attention := registry.Attention(); len(attention) != 0 {
		t.Fatalf("an answered ask still wants attention: %v", attention)
	}
	// The consumer resuming from the first frame reads the rest of the log
	// before anything live; the driver holds the replay to its one order.
	late := attach(ctx, t, carrier, registry, "go-relay", session.Observer, "late")
	if late.Role != session.Observer {
		t.Fatalf("the resuming consumer attached as %s", late.Role)
	}
	speaker.send(gateLive)
	speaker.takes(gateReplayed)
	speaker.quiet()
	// The machine's channel closing ends every attached channel with its own
	// close, and the session is gone from the registry.
	speaker.close(duplex.CodePolicyViolation, gateGone)
	deadline := time.Now().Add(10 * time.Second)
	for registry.Control("go-relay", nil) == nil {
		if time.Now().After(deadline) {
			t.Fatal("the registry still holds a session whose machine went away")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// typeScriptRelay is the gate the other way: the session is bound on the
// driver's registry and every consumer of it is this side's, over channels
// this side opens in the order the driver attaches them.
func typeScriptRelay(ctx context.Context, t *testing.T, carrier *tunnel.Tunnel) {
	t.Helper()
	one := open(ctx, t, carrier, "one", 0)
	two := open(ctx, t, carrier, "two", 0)
	one.takes(gateReady)
	two.takes(gateReady)
	// A response reaches the one consumer that asked, under the id it asked
	// with, and no other consumer reads it.
	one.send(gateEcho)
	one.takes(gateAnswered)
	// An ask reaches the holder, which says so; control moves while it is
	// open, and the ask follows it as the machine sent it.
	one.takes(gateAsk)
	one.send(gateNoticed)
	two.takes(gateAsk)
	one.send(gateStale)
	two.send(gateHeld)
	// A consumer resuming from the first frame reads the log from there, in
	// one order, before the frame the machine sends once it is attached.
	late := open(ctx, t, carrier, "late", 1)
	for _, frame := range []string{gateMinted, gateAnswer, gateAsk, gateNoticed, gateHeld, gateLive} {
		late.takes(frame)
	}
	// That live frame reached every attached consumer, and those two read
	// nothing else: the exchange one was part of never reached two.
	one.takes(gateLive)
	two.takes(gateLive)
	two.quiet()
	one.send(gateReplayed)
	// The machine's channel closing ends every attached channel with its
	// code and its reason.
	for _, consumer := range []*end{one, two, late} {
		closed := consumer.ended()
		if closed.Code != duplex.CodePolicyViolation || closed.Reason != gateGone {
			t.Fatalf("%s ended as %d %q", consumer.name, closed.Code, closed.Reason)
		}
	}
}

// attach takes the next channel the driver opened and adds it to the session
// as a consumer, resuming from the sequence the open itself carried: the
// tunnel carries after and never acts on it, and this is where it is acted
// on.
func attach(ctx context.Context, t *testing.T, carrier *tunnel.Tunnel, registry *session.Registry, id string, role session.Role, origin string) *session.Attachment {
	t.Helper()
	channel, err := carrier.Accept(ctx)
	if err != nil {
		t.Fatalf("the driver opened no channel for %s: %v", origin, err)
	}
	if channel.Family != "probe" {
		t.Fatalf("the driver opened a channel of family %q for %s", channel.Family, origin)
	}
	attachment, err := registry.Attach(id, channel, role, origin, channel.After)
	if err != nil {
		t.Fatal(err)
	}
	return attachment
}

// open gives the driver's registry a channel to attach a consumer of this
// side over, saying the sequence this side holds.
func open(ctx context.Context, t *testing.T, carrier *tunnel.Tunnel, name string, after int64) *end {
	t.Helper()
	channel, err := carrier.Open(ctx, "probe", after)
	if err != nil {
		t.Fatalf("no channel for %s: %v", name, err)
	}
	return &end{t: t, name: name, channel: channel}
}

// end is one end of a channel the gate speaks the profile over: a machine's,
// or a consumer's. It holds a frame to the text it was sent as, which is
// what a relay that rewrites an id and nothing else promises.
type end struct {
	t       *testing.T
	name    string
	channel *tunnel.Channel
}

func (e *end) send(frame string) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.channel.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: []byte(frame)}); err != nil {
		e.t.Fatalf("%s could not send: %v", e.name, err)
	}
}

func (e *end) takes(frame string) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	received, err := e.channel.Receive(ctx)
	if err != nil {
		e.t.Fatalf("%s received nothing where %s was due: %v", e.name, frame, err)
	}
	if string(received.Data) != frame {
		e.t.Fatalf("%s received\n\t%s\nand not\n\t%s", e.name, received.Data, frame)
	}
}

// quiet holds that nothing more reaches this end: what a relay refuses, or
// routes elsewhere, arrives nowhere.
func (e *end) quiet() {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if received, err := e.channel.Receive(ctx); err == nil {
		e.t.Fatalf("%s received %s", e.name, received.Data)
	}
}

func (e *end) close(code duplex.Code, reason string) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.channel.Close(ctx, code, reason); err != nil {
		e.t.Fatalf("%s could not close: %v", e.name, err)
	}
}

// ended waits for this end's channel to be closed and returns the close.
func (e *end) ended() *duplex.CloseError {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	received, err := e.channel.Receive(ctx)
	if err == nil {
		e.t.Fatalf("%s received %s where its channel should have ended", e.name, received.Data)
	}
	var closed *duplex.CloseError
	if !errors.As(err, &closed) {
		e.t.Fatalf("%s ended with %v", e.name, err)
	}
	return closed
}

// transcript takes the driver's output while it runs, so that a gate failing
// before the driver ends may quote what it had said.
type transcript struct {
	mu   sync.Mutex
	said bytes.Buffer
}

func (x *transcript) Write(p []byte) (int, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.said.Write(p)
}

func (x *transcript) String() string {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.said.String()
}
