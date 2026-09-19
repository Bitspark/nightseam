// Package sessiontest holds a session component to what a relay promises.
// An implementation's own tests call Run with a way to make a connected
// pair of the seam's connections; what Run checks is the routing table of
// the boundary rule and nothing of the transport beneath it, so a consumer
// written against the component can trust the same things wherever it runs
// — over a tunnel channel, over the seam's pipe, over a bare socket: a
// response reaches the one consumer that asked, an event reaches every
// consumer, control decides what a consumer may send, an ask follows
// control, ids are the session's own, a consumer resumes from the log, and
// a connection ending ends what it should and nothing more.
//
// The family the suite governs by is the generator's own probe — decides
// echo, asks reverse — read from the corpus by the tool's loader, so the
// suite is held to what the corpus declares rather than to a copy of it.
package sessiontest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/session/go"
)

// Connect makes one connected pair of the seam's connections carrying the
// probe family: near is the end a registry binds or attaches, far the end a
// machine or a consumer speaks over. observer is what a session bound over
// near is to tell, and is nil where the suite is not watching: a transport
// whose connections run over a peer — a tunnel's channels — gives it to the
// peer near runs over, and one whose connections run over none — the seam's
// pipe — ignores it, the suite giving the registry the same observer, which
// is the order a relay reads the two in. The suite closes what it opened;
// the transport may register cleanup with t.
type Connect func(t *testing.T, observer runtime.Observer) (near, far duplex.Conn)

// Run holds a session component to the relay's contract.
func Run(t *testing.T, connect Connect) {
	t.Helper()
	governance := Probe(t)

	// observedBy makes a registry under the limits a scenario asks for,
	// telling observer what its sessions do. A transport whose connections
	// run over a peer seats the same observer there, where a relay reads it
	// first; one whose connections do not leaves this the only place it is,
	// which is the precedence stated and the reason both are given it.
	observedBy := func(options session.Options, observer runtime.Observer) *session.Registry {
		options.Observer = observer
		return session.New(options)
	}

	// bind makes a registry with one session bound, and returns the machine's
	// end of its channel.
	bind := func(t *testing.T, id string) (*session.Registry, *speaker) {
		t.Helper()
		registry := session.New(session.Options{})
		near, far := connect(t, nil)
		if err := registry.Bind(id, near, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		return registry, newSpeaker("machine", far, false)
	}
	attach := func(t *testing.T, registry *session.Registry, id, origin string, role session.Role, after int64) (*session.Attachment, *speaker) {
		t.Helper()
		near, far := connect(t, nil)
		// The consumer is reading before the attach, which sends it who holds
		// control and everything it missed before it returns: a replay is
		// longer than a connection holds.
		consumer := newSpeaker(origin, far, true)
		attachment, err := registry.Attach(id, near, role, origin, after)
		if err != nil {
			t.Fatal(err)
		}
		// Every consumer is told who holds control before anything else on
		// its connection; it is taken here so that what follows is the
		// session's conversation, and the case below holds what it said.
		consumer.joined(t)
		return attachment, consumer
	}

	t.Run("a response reaches the one consumer that asked, an event every one", func(t *testing.T) {
		registry, machine := bind(t, "s")
		_, one := attach(t, registry, "s", "one", session.Participant, 0)
		_, two := attach(t, registry, "s", "two", session.Observer, 0)
		one.send(t, `{"version":1,"kind":"request","id":"c:7","method":"no_args","params":{}}`)
		asked := machine.take(t)
		if asked.text("method") != "no_args" || asked.text("id") == "c:7" {
			t.Fatalf("the machine saw %s", asked.raw)
		}
		two.quiet(t)
		machine.send(t, fmt.Sprintf(`{"version":1,"kind":"response","id":%q,"result":"ok"}`, asked.text("id")))
		answered := one.take(t)
		if answered.text("id") != "c:7" || answered.text("result") != "ok" {
			t.Fatalf("the consumer that asked saw %s", answered.raw)
		}
		two.quiet(t)
		machine.send(t, `{"version":1,"kind":"event","event":"changed","data":{"text":"t","count":1}}`)
		for _, consumer := range []*speaker{one, two} {
			if event := consumer.take(t); event.text("event") != "changed" {
				t.Fatalf("%s saw %s", consumer.name, event.raw)
			}
		}
	})

	t.Run("a deciding frame is the holder's alone", func(t *testing.T) {
		registry, machine := bind(t, "s")
		holder, first := attach(t, registry, "s", "first", session.Participant, 0)
		_, second := attach(t, registry, "s", "second", session.Participant, 0)
		_, watcher := attach(t, registry, "s", "watcher", session.Observer, 0)
		const echo = `{"version":1,"kind":"request","id":"c:1","method":"echo","params":{"text":"t","count":1}}`
		// An observer, and a participant that does not hold control, are
		// refused; the machine never sees either.
		for _, consumer := range []*speaker{watcher, second} {
			consumer.send(t, echo)
			refused := consumer.alone(t)
			if refused.text("kind") != "response" || refused.text("id") != "c:1" {
				t.Fatalf("%s saw %s", consumer.name, refused.raw)
			}
			var public struct{ Code string }
			if err := json.Unmarshal(refused.member["error"], &public); err != nil {
				t.Fatal(err)
			}
			if public.Code != session.ErrorNotControlling {
				t.Fatalf("%s was refused with %q", consumer.name, public.Code)
			}
		}
		machine.quiet(t)
		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		// Control moving is told to every consumer, the one it moved to and
		// the ones it did not.
		for _, consumer := range []*speaker{first, second, watcher} {
			if origin, held := consumer.control(t); origin != "first" || !held {
				t.Fatalf("%s was told control is %q, held %v", consumer.name, origin, held)
			}
		}
		first.send(t, echo)
		asked := machine.take(t)
		if asked.text("method") != "echo" {
			t.Fatalf("the machine saw %s", asked.raw)
		}
		// A cancel decides: one that does not hold control is dropped, the
		// holder's reaches the machine under the session's id.
		second.send(t, `{"version":1,"kind":"cancel","id":"c:1"}`)
		machine.quiet(t)
		first.send(t, `{"version":1,"kind":"cancel","id":"c:1"}`)
		withdrawn := machine.take(t)
		if withdrawn.text("kind") != "cancel" || withdrawn.text("id") != asked.text("id") {
			t.Fatalf("the machine saw %s", withdrawn.raw)
		}
		// An answer to what the machine asked decides too.
		machine.send(t, `{"version":1,"kind":"request","id":"s:1","method":"reverse","params":{"text":"t","count":1}}`)
		if ask := first.take(t); ask.text("id") != "s:1" {
			t.Fatalf("the holder saw %s", ask.raw)
		}
		second.send(t, `{"version":1,"kind":"response","id":"s:1","result":{"text":"other","count":1}}`)
		machine.quiet(t)
		first.send(t, `{"version":1,"kind":"response","id":"s:1","result":{"text":"held","count":1}}`)
		if answered := machine.take(t); answered.text("id") != "s:1" {
			t.Fatalf("the machine saw %s", answered.raw)
		}
	})

	t.Run("an ask reaches the holder and follows a transfer while open", func(t *testing.T) {
		registry, machine := bind(t, "s")
		first, one := attach(t, registry, "s", "one", session.Participant, 0)
		second, two := attach(t, registry, "s", "two", session.Participant, 0)
		if err := registry.Control("s", first); err != nil {
			t.Fatal(err)
		}
		one.control(t)
		two.control(t)
		machine.send(t, `{"version":1,"kind":"request","id":"s:1","method":"reverse","params":{"text":"t","count":1}}`)
		if ask := one.take(t); ask.text("method") != "reverse" {
			t.Fatalf("the holder saw %s", ask.raw)
		}
		two.quiet(t)
		if err := registry.Control("s", second); err != nil {
			t.Fatal(err)
		}
		one.control(t)
		if origin, held := two.control(t); origin != "two" || !held {
			t.Fatalf("the new holder was told control is %q, held %v", origin, held)
		}
		// The ask follows control as the frame the log already holds: handed
		// again is no new place in the order, so it carries no cursor and
		// never moves the new holder's backwards.
		if ask := two.alone(t); ask.text("id") != "s:1" || ask.text("method") != "reverse" {
			t.Fatalf("the new holder saw %s", ask.raw)
		}
		// The consumer that no longer holds control no longer answers.
		one.send(t, `{"version":1,"kind":"response","id":"s:1","result":{"text":"stale","count":1}}`)
		machine.quiet(t)
		two.send(t, `{"version":1,"kind":"response","id":"s:1","result":{"text":"held","count":1}}`)
		answered := machine.take(t)
		if string(answered.member["result"]) != `{"text":"held","count":1}` {
			t.Fatalf("the machine saw %s", answered.raw)
		}
		// An observer is never given control.
		watcher, _ := attach(t, registry, "s", "watcher", session.Observer, 0)
		if err := registry.Control("s", watcher); err == nil {
			t.Fatal("an observer was given control")
		}
	})

	t.Run("the session's ids are its own across consumers that both mint c:1", func(t *testing.T) {
		registry, machine := bind(t, "s")
		_, one := attach(t, registry, "s", "one", session.Participant, 0)
		_, two := attach(t, registry, "s", "two", session.Participant, 0)
		one.send(t, `{"version":1,"kind":"request","id":"c:1","method":"no_args","params":{}}`)
		first := machine.take(t)
		two.send(t, `{"version":1,"kind":"request","id":"c:1","method":"no_args","params":{}}`)
		second := machine.take(t)
		if first.text("id") == second.text("id") {
			t.Fatalf("two consumers asked under the one id %s", first.text("id"))
		}
		machine.send(t, fmt.Sprintf(`{"version":1,"kind":"response","id":%q,"result":"second"}`, second.text("id")))
		machine.send(t, fmt.Sprintf(`{"version":1,"kind":"response","id":%q,"result":"first"}`, first.text("id")))
		if answered := two.take(t); answered.text("id") != "c:1" || answered.text("result") != "second" {
			t.Fatalf("the second consumer saw %s", answered.raw)
		}
		if answered := one.take(t); answered.text("id") != "c:1" || answered.text("result") != "first" {
			t.Fatalf("the first consumer saw %s", answered.raw)
		}
	})

	t.Run("a consumer resumes from the log, before any live frame", func(t *testing.T) {
		registry, machine := bind(t, "s")
		_, one := attach(t, registry, "s", "one", session.Participant, 0)
		one.send(t, `{"version":1,"kind":"request","id":"c:1","method":"no_args","params":{}}`)
		asked := machine.take(t)
		machine.send(t, fmt.Sprintf(`{"version":1,"kind":"response","id":%q,"result":"ok"}`, asked.text("id")))
		one.take(t)
		machine.send(t, `{"version":1,"kind":"event","event":"changed","data":{"text":"one","count":1}}`)
		one.take(t)
		// Three frames are in the log — the request as the machine saw it, its
		// response, and the event — and one of them is the machine's event,
		// which is the whole of what a replay hands a channel that speaks the
		// family. The other two are the one consumer's conversation with the
		// machine, under ids of the session's own.
		_, late := attach(t, registry, "s", "late", session.Observer, 0)
		replayed := late.take(t)
		if replayed.text("event") != "changed" || string(replayed.member["data"]) != `{"text":"one","count":1}` {
			t.Fatalf("the replay gave %s", replayed.raw)
		}
		if late.cursor != 3 {
			t.Fatalf("the replayed event left the consumer standing at %d", late.cursor)
		}
		late.quiet(t)
		machine.send(t, `{"version":1,"kind":"event","event":"changed","data":{"text":"two","count":2}}`)
		if live := late.take(t); string(live.member["data"]) != `{"text":"two","count":2}` {
			t.Fatalf("the live frame after the replay was %s", live.raw)
		}
		// A consumer that holds the first two frames resumes at the third.
		_, later := attach(t, registry, "s", "later", session.Observer, 2)
		if resumed := later.take(t); resumed.text("event") != "changed" || string(resumed.member["data"]) != `{"text":"one","count":1}` {
			t.Fatalf("a consumer resuming after two frames saw %s", resumed.raw)
		}
		if later.cursor != 3 {
			t.Fatalf("a consumer replayed the third frame was told it stands at %d", later.cursor)
		}
	})

	t.Run("a replay hands a consumer the machine's events alone, and the cursor passes what it was not given", func(t *testing.T) {
		// The log holds every kind of frame a session records, and a replay is
		// not a reading of it: a channel that speaks the family takes what the
		// machine sent down to every consumer, and nothing that went up, an up
		// frame on a down channel being a request from the wrong side.
		registry, machine := bind(t, "s")
		holder, one := attach(t, registry, "s", "one", session.Participant, 0)
		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		one.control(t)
		one.send(t, `{"version":1,"kind":"request","id":"c:1","method":"echo","params":{"text":"t","count":1}}`)
		asked := machine.take(t)
		machine.send(t, fmt.Sprintf(`{"version":1,"kind":"response","id":%q,"result":{"text":"t","count":1}}`, asked.text("id")))
		one.take(t)
		machine.send(t, `{"version":1,"kind":"event","event":"changed","data":{"text":"one","count":1}}`)
		one.take(t)
		machine.send(t, `{"version":1,"kind":"request","id":"s:1","method":"reverse","params":{"text":"t","count":1}}`)
		one.take(t)
		one.send(t, `{"version":1,"kind":"response","id":"s:1","result":{"text":"t","count":1}}`)
		machine.take(t)
		machine.send(t, `{"version":1,"kind":"event","event":"changed","data":{"text":"two","count":2}}`)
		one.take(t)
		// Six frames, of which the machine's two events are what a consumer
		// attaching is given; the sixth being one of them, the replay ends on a
		// frame it delivered and the cursor that names it.
		_, late := attach(t, registry, "s", "late", session.Observer, 0)
		for _, want := range []string{`{"text":"one","count":1}`, `{"text":"two","count":2}`} {
			replayed := late.take(t)
			if replayed.text("event") != "changed" || string(replayed.member["data"]) != want {
				t.Fatalf("the replay gave %s, where the event carrying %s was due", replayed.raw, want)
			}
		}
		if late.cursor != 6 {
			t.Fatalf("the replay left the consumer standing at %d", late.cursor)
		}
		late.quiet(t)
		// A frame that went up after the last event is passed over all the
		// same, and the replay ends by saying where it reached: a consumer
		// resuming from that cursor reads the log on rather than over the
		// frames it was never given.
		one.send(t, `{"version":1,"kind":"request","id":"c:2","method":"echo","params":{"text":"u","count":1}}`)
		machine.take(t)
		_, last := attach(t, registry, "s", "last", session.Observer, 6)
		if stood := last.stands(t); stood != 7 {
			t.Fatalf("a replay that gave nothing ended naming %d", stood)
		}
		last.quiet(t)
		// And a consumer whose end is a peer of the profile lives through it.
		// A request of another consumer's carries an id the session minted for
		// the machine — c:N, the prefix a consumer's own peer mints under — so
		// a peer handed one as a request of the machine's ends the connection
		// on the prefix, and the consumer that attached from nothing is left
		// with the first frames of the replay and no session.
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		near, far := connect(t, nil)
		peer, err := runtime.NewPeer(ctx, far, runtime.ClientRole, runtime.Options{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = peer.Close() })
		arrived := make(chan string, 8)
		peer.HandleEvent("changed", func(_ context.Context, _ *runtime.Peer, data json.RawMessage) { arrived <- string(data) })
		if _, err := registry.Attach("s", near, session.Observer, "peer", 0); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`{"text":"one","count":1}`, `{"text":"two","count":2}`} {
			select {
			case got := <-arrived:
				if got != want {
					t.Fatalf("the consumer's peer was given %s, where %s was due", got, want)
				}
			case <-peer.Done():
				t.Fatalf("the consumer's peer ended during the replay: %v", peer.Err())
			case <-time.After(5 * time.Second):
				t.Fatalf("the replay reached the consumer's peer with nothing carrying %s", want)
			}
		}
		select {
		case <-peer.Done():
			t.Fatalf("the consumer's peer ended after the replay: %v", peer.Err())
		case <-time.After(250 * time.Millisecond):
		}
	})

	t.Run("a session bound over a log that already holds frames is bound at its head", func(t *testing.T) {
		// The log is filled through the interface a consumer's own durable one
		// implements, so that what is held here is what a session bound after
		// a restart is bound over, and the suite needs no log of its own.
		held := []string{
			`{"version":1,"kind":"event","event":"changed","data":{"text":"one","count":1}}`,
			`{"version":1,"kind":"event","event":"changed","data":{"text":"two","count":2}}`,
			`{"version":1,"kind":"event","event":"changed","data":{"text":"three","count":3}}`,
		}
		log := session.NewMemoryLog(0)
		for _, message := range held {
			if _, err := log.Append(context.Background(), session.Frame{Direction: session.Down, Message: []byte(message)}); err != nil {
				t.Fatal(err)
			}
		}
		registry := session.New(session.Options{})
		near, far := connect(t, nil)
		if err := registry.Bind("s", near, governance, log); err != nil {
			t.Fatal(err)
		}
		machine := newSpeaker("machine", far, false)
		// A consumer resuming from nothing, before the machine has spoken at
		// all, is given every frame the log holds.
		_, all := attach(t, registry, "s", "all", session.Observer, 0)
		for i, message := range held {
			if replayed := all.take(t); string(replayed.raw) != message {
				t.Fatalf("frame %d of the replay was %s", i+1, replayed.raw)
			}
		}
		all.quiet(t)
		// And one holding all but the last two takes exactly those two.
		_, late := attach(t, registry, "s", "late", session.Observer, int64(len(held)-2))
		for _, message := range held[len(held)-2:] {
			if replayed := late.take(t); string(replayed.raw) != message {
				t.Fatalf("a consumer resuming after %d frames saw %s", len(held)-2, replayed.raw)
			}
		}
		late.quiet(t)
		// The session goes on from the log's end rather than from nothing: the
		// machine's next frame takes the sequence after the head, which is what
		// a consumer holding the whole log is replayed nothing before.
		const live = `{"version":1,"kind":"event","event":"changed","data":{"text":"live","count":4}}`
		machine.send(t, live)
		for _, consumer := range []*speaker{all, late} {
			if received := consumer.take(t); string(received.raw) != live {
				t.Fatalf("%s saw %s", consumer.name, received.raw)
			}
		}
		_, after := attach(t, registry, "s", "after", session.Observer, int64(len(held)))
		if replayed := after.take(t); string(replayed.raw) != live {
			t.Fatalf("a consumer resuming after the whole log saw %s", replayed.raw)
		}
		after.quiet(t)
	})

	t.Run("a frame carrying members the relay does not know arrives with them", func(t *testing.T) {
		registry, machine := bind(t, "s")
		holder, one := attach(t, registry, "s", "one", session.Participant, 0)
		_, two := attach(t, registry, "s", "two", session.Observer, 0)
		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		one.control(t)
		two.control(t)
		const sent = `{"version":1,"kind":"request","id":"c:9","traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01","method":"echo","params":{"text":"t","count":1},"baggage":{"tenant":"acme"}}`
		one.send(t, sent)
		asked := machine.take(t)
		if asked.text("id") == "c:9" {
			t.Fatal("the relay did not mint an id of the session's own")
		}
		intact(t, sent, asked)
		// And down the other way, where nothing is rewritten at all.
		const emitted = `{"version":1,"kind":"event","event":"changed","tracestate":"nightseam=1","data":{"text":"t","count":1},"extension":[1,2]}`
		machine.send(t, emitted)
		for _, consumer := range []*speaker{one, two} {
			received := consumer.take(t)
			if string(received.raw) != emitted {
				t.Fatalf("%s saw %s", consumer.name, received.raw)
			}
		}
	})

	t.Run("a frame's meta reaches the machine and every consumer verbatim", func(t *testing.T) {
		// The relay forwards the profile's carriage as it forwards a member it
		// does not know: it is the consumer's and the machine's, and nothing
		// between them reads, rewrites or strips it.
		registry, machine := bind(t, "s")
		holder, one := attach(t, registry, "s", "one", session.Participant, 0)
		_, two := attach(t, registry, "s", "two", session.Observer, 0)
		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		one.control(t)
		two.control(t)
		const sent = `{"version":1,"kind":"request","id":"c:9","method":"echo","params":{"text":"t","count":1},"meta":{"tenant":"acme","idempotency":"k-1"}}`
		one.send(t, sent)
		asked := machine.take(t)
		intact(t, sent, asked)
		const emitted = `{"version":1,"kind":"event","event":"changed","data":{"text":"t","count":1},"meta":{"cause":"nightly"}}`
		machine.send(t, emitted)
		for _, consumer := range []*speaker{one, two} {
			if received := consumer.take(t); string(received.raw) != emitted {
				t.Fatalf("%s saw %s", consumer.name, received.raw)
			}
		}
		// The log keeps each message whole, so a consumer that was not there is
		// replayed the carriage with it: the event, which is what a replay
		// hands a channel. The request the relay recorded on its way to the
		// machine is in the log with its own all the same — the replay passes
		// over it and the cursor passes it, which is why a meta that holds a
		// credential wants a Log that redacts, as docs/session.md says.
		_, late := attach(t, registry, "s", "late", session.Observer, 0)
		if replayed := late.take(t); string(replayed.raw) != emitted {
			t.Fatalf("the log replayed the event as %s", replayed.raw)
		}
		if late.cursor != 2 {
			t.Fatalf("the replayed event left the consumer standing at %d, where the request it was not given is the first frame", late.cursor)
		}
	})

	t.Run("the machine's channel ends every consumer, a consumer's only itself", func(t *testing.T) {
		registry, machine := bind(t, "s")
		_, one := attach(t, registry, "s", "one", session.Participant, 0)
		_, two := attach(t, registry, "s", "two", session.Observer, 0)
		// A consumer closing detaches it and nothing else.
		two.close(t, duplex.CodeNormal, "done")
		machine.send(t, `{"version":1,"kind":"event","event":"changed","data":{"text":"t","count":1}}`)
		if event := one.take(t); event.text("event") != "changed" {
			t.Fatalf("the session did not go on: %s", event.raw)
		}
		// The machine's channel closing ends what is attached, with its close.
		machine.close(t, duplex.CodePolicyViolation, "the machine went away")
		closed := one.ended(t)
		if closed.Code != duplex.CodePolicyViolation || closed.Reason != "the machine went away" {
			t.Fatalf("the consumer's channel ended as %d %q", closed.Code, closed.Reason)
		}
		if _, err := registry.Attach("s", mustConnection(t, connect), session.Participant, "late", 0); err == nil {
			t.Fatal("a consumer attached to an ended session")
		}
	})

	t.Run("attention is every session the machine asked of", func(t *testing.T) {
		registry := session.New(session.Options{})
		machines := map[string]*speaker{}
		holders := map[string]*speaker{}
		for _, id := range []string{"one", "two"} {
			near, far := connect(t, nil)
			if err := registry.Bind(id, near, governance, session.NewMemoryLog(0)); err != nil {
				t.Fatal(err)
			}
			machines[id] = newSpeaker("machine "+id, far, false)
			down, consumer := connect(t, nil)
			held := newSpeaker(id, consumer, true)
			attachment, err := registry.Attach(id, down, session.Participant, id, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := registry.Control(id, attachment); err != nil {
				t.Fatal(err)
			}
			held.joined(t)
			held.control(t)
			holders[id] = held
		}
		if attention := registry.Attention(); len(attention) != 0 {
			t.Fatalf("a quiet registry wants attention: %v", attention)
		}
		// A request the family does not ask is routed but is not an ask.
		machines["one"].send(t, `{"version":1,"kind":"request","id":"s:1","method":"unasked","params":{}}`)
		holders["one"].take(t)
		if attention := registry.Attention(); len(attention) != 0 {
			t.Fatalf("a request the family does not ask wants attention: %v", attention)
		}
		machines["two"].send(t, `{"version":1,"kind":"request","id":"s:2","method":"reverse","params":{"text":"t","count":1}}`)
		holders["two"].take(t)
		if attention := registry.Attention(); len(attention) != 1 || attention[0] != "two" {
			t.Fatalf("attention is %v", attention)
		}
		holders["two"].send(t, `{"version":1,"kind":"response","id":"s:2","result":{"text":"t","count":1}}`)
		machines["two"].take(t)
		if attention := registry.Attention(); len(attention) != 0 {
			t.Fatalf("an answered ask still wants attention: %v", attention)
		}
	})

	t.Run("every domain change of a session reaches OnChange and the session's observer, in the order the registry made them", func(t *testing.T) {
		// One session from bound to unbound, read twice over: the change the
		// registry told and the event the observer of the machine's own side
		// was given, which are one domain change said twice. The scenarios
		// above, in one run.
		events := observing()
		registry := observedBy(session.Options{}, events)
		changes := watching(t, registry, "s")
		near, far := connect(t, events)
		if err := registry.Bind("s", near, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		machine := newSpeaker("machine", far, false)
		changes.expect(t, expected{kind: session.ChangeBound})
		events.expect(t, session.SessionBound{Session: "s"})

		// Two consumers, each resuming from the beginning of a log that has
		// nothing in it yet.
		holder, one := attach(t, registry, "s", "one", session.Participant, 0)
		changes.expect(t, expected{kind: session.ChangeAttached, origin: "one"})
		events.expect(t, session.SessionAttached{Session: "s", Role: session.Participant, Origin: "one"})
		_, two := attach(t, registry, "s", "two", session.Observer, 0)
		changes.expect(t, expected{kind: session.ChangeAttached, origin: "two"})
		events.expect(t, session.SessionAttached{Session: "s", Role: session.Observer, Origin: "two"})

		// A request neither side governs, and its answer: two frames, and
		// the consumer that sent the first is the change's own. A frame says
		// how large it was and never what was in it.
		one.send(t, `{"version":1,"kind":"request","id":"c:7","method":"no_args","params":{}}`)
		asked := machine.take(t)
		changes.expect(t, expected{kind: session.ChangeFrameAppended, origin: "one", method: "no_args", sequence: 1})
		events.expect(t, session.FrameAppended{Session: "s", Sequence: 1, Direction: session.Up,
			Origin: "one", Bytes: len(asked.raw), Method: "no_args"})
		answer := fmt.Sprintf(`{"version":1,"kind":"response","id":%q,"result":"ok"}`, asked.text("id"))
		machine.send(t, answer)
		one.take(t)
		changes.expect(t, expected{kind: session.ChangeFrameAppended, sequence: 2})
		events.expect(t, session.FrameAppended{Session: "s", Sequence: 2, Direction: session.Down, Bytes: len(answer)})
		const emitted = `{"version":1,"kind":"event","event":"changed","data":{"text":"t","count":1}}`
		machine.send(t, emitted)
		one.take(t)
		two.take(t)
		changes.expect(t, expected{kind: session.ChangeFrameAppended, method: "changed", sequence: 3})
		events.expect(t, session.FrameAppended{Session: "s", Sequence: 3, Direction: session.Down,
			Bytes: len(emitted), Method: "changed"})

		// What the relay refuses is a change and never a frame: the machine
		// never saw it, so the log did not either.
		two.send(t, `{"version":1,"kind":"request","id":"c:1","method":"echo","params":{"text":"t","count":1}}`)
		two.alone(t)
		changes.expect(t, expected{kind: session.ChangeRefused, origin: "two", method: "echo"})
		events.expect(t, session.Refused{Session: "s", Code: session.ErrorNotControlling, Method: "echo",
			Role: session.Observer, Origin: "two"})

		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		changes.expect(t, expected{kind: session.ChangeControlChanged, origin: "one"})
		events.expect(t, session.ControlChanged{Session: "s", Origin: "one", Held: true})
		one.control(t)
		two.control(t)

		// A deciding frame of the holder's, with a trace the relay knows
		// nothing of and tells whoever is watching about.
		one.send(t, fmt.Sprintf(`{"version":1,"kind":"request","id":"c:1","method":"echo","traceparent":%q,"params":{"text":"t","count":1}}`, firstTrace))
		echoed := machine.take(t)
		changes.expect(t, expected{kind: session.ChangeFrameAppended, origin: "one", method: "echo", sequence: 4, trace: firstTrace})
		events.expect(t, session.FrameAppended{Session: "s", Sequence: 4, Direction: session.Up, Origin: "one",
			Bytes: len(echoed.raw), Method: "echo", Trace: runtime.Trace{Parent: firstTrace}})
		echoAnswer := fmt.Sprintf(`{"version":1,"kind":"response","id":%q,"result":{"text":"t","count":1}}`, echoed.text("id"))
		machine.send(t, echoAnswer)
		one.take(t)
		changes.expect(t, expected{kind: session.ChangeFrameAppended, sequence: 5})
		events.expect(t, session.FrameAppended{Session: "s", Sequence: 5, Direction: session.Down, Bytes: len(echoAnswer)})

		// A request the machine opens that the family does not ask is raised
		// and routed like any other, Asking saying which it was: what Asks
		// selects is what Attention names, and the holder is left standing
		// with this one all the same.
		const unasked = `{"version":1,"kind":"request","id":"s:0","method":"unasked","params":{}}`
		machine.send(t, unasked)
		one.take(t)
		changes.expect(t, expected{kind: session.ChangeFrameAppended, method: "unasked", sequence: 6})
		events.expect(t, session.FrameAppended{Session: "s", Sequence: 6, Direction: session.Down,
			Bytes: len(unasked), Method: "unasked"})
		changes.expect(t, expected{kind: session.ChangeAskRaised, method: "unasked"})
		events.expect(t, session.AskRaised{Session: "s", ID: "s:0", Method: "unasked"})
		changes.expect(t, expected{kind: session.ChangeAskRouted, origin: "one", method: "unasked"})
		events.expect(t, session.AskRouted{Session: "s", ID: "s:0", Method: "unasked", Origin: "one"})
		one.send(t, `{"version":1,"kind":"response","id":"s:0","result":"done"}`)
		closed := machine.take(t)
		changes.expect(t, expected{kind: session.ChangeAskAnswered, origin: "one", method: "unasked"})
		events.expect(t, session.AskAnswered{Session: "s", ID: "s:0", Method: "unasked", Origin: "one"})
		changes.expect(t, expected{kind: session.ChangeFrameAppended, origin: "one", sequence: 7})
		events.expect(t, session.FrameAppended{Session: "s", Sequence: 7, Direction: session.Up,
			Origin: "one", Bytes: len(closed.raw)})

		// And one the family does ask, which the transfer below carries.
		asking := fmt.Sprintf(`{"version":1,"kind":"request","id":"s:1","method":"reverse","traceparent":%q,"params":{"text":"t","count":1}}`, askTrace)
		machine.send(t, asking)
		one.take(t)
		changes.expect(t, expected{kind: session.ChangeFrameAppended, method: "reverse", sequence: 8, trace: askTrace})
		events.expect(t, session.FrameAppended{Session: "s", Sequence: 8, Direction: session.Down, Bytes: len(asking),
			Method: "reverse", Trace: runtime.Trace{Parent: askTrace}})
		changes.expect(t, expected{kind: session.ChangeAskRaised, method: "reverse", trace: askTrace})
		events.expect(t, session.AskRaised{Session: "s", ID: "s:1", Method: "reverse", Asking: true,
			Trace: runtime.Trace{Parent: askTrace}})
		changes.expect(t, expected{kind: session.ChangeAskRouted, origin: "one", method: "reverse", trace: askTrace})
		events.expect(t, session.AskRouted{Session: "s", ID: "s:1", Method: "reverse", Origin: "one",
			Trace: runtime.Trace{Parent: askTrace}})

		// A consumer resuming from nothing takes the one of the log's eight
		// frames a channel of the family can take — the event — and then the
		// cursor that ends the replay where it reached. Neither is a change of
		// the session's: a replay is what one consumer is told, not something
		// that happened to the session.
		next, three := attach(t, registry, "s", "three", session.Participant, 0)
		changes.expect(t, expected{kind: session.ChangeAttached, origin: "three"})
		events.expect(t, session.SessionAttached{Session: "s", Role: session.Participant, Origin: "three"})
		if replayed := three.take(t); string(replayed.raw) != emitted {
			t.Fatalf("the replay gave %s", replayed.raw)
		}
		if stood := three.stands(t); stood != 8 {
			t.Fatalf("the replay ended naming %d, where the log stands at eight", stood)
		}

		// Control moving asks the open request afresh of whoever holds it
		// now, and both are changes.
		if err := registry.Control("s", next); err != nil {
			t.Fatal(err)
		}
		changes.expect(t, expected{kind: session.ChangeControlChanged, origin: "three"})
		events.expect(t, session.ControlChanged{Session: "s", Origin: "three", Held: true})
		for _, consumer := range []*speaker{one, two, three} {
			consumer.control(t)
		}
		three.alone(t)
		changes.expect(t, expected{kind: session.ChangeAskRouted, origin: "three", method: "reverse", trace: askTrace})
		events.expect(t, session.AskRouted{Session: "s", ID: "s:1", Method: "reverse", Origin: "three",
			Trace: runtime.Trace{Parent: askTrace}})

		// The answer closes the request under the method it was opened with,
		// carrying the trace the answer itself came with, before the frame
		// it answers with reaches the log.
		three.send(t, fmt.Sprintf(`{"version":1,"kind":"response","id":"s:1","traceparent":%q,"result":{"text":"t","count":1}}`, answerTrace))
		answered := machine.take(t)
		changes.expect(t, expected{kind: session.ChangeAskAnswered, origin: "three", method: "reverse", trace: answerTrace})
		events.expect(t, session.AskAnswered{Session: "s", ID: "s:1", Method: "reverse", Origin: "three",
			Trace: runtime.Trace{Parent: answerTrace}})
		changes.expect(t, expected{kind: session.ChangeFrameAppended, origin: "three", sequence: 9, trace: answerTrace})
		events.expect(t, session.FrameAppended{Session: "s", Sequence: 9, Direction: session.Up, Origin: "three",
			Bytes: len(answered.raw), Trace: runtime.Trace{Parent: answerTrace}})

		// A consumer that leaves holding nothing leaves one change behind,
		// and one that leaves holding control leaves the change releasing it
		// by hand would have made first.
		holder.Detach()
		changes.expect(t, expected{kind: session.ChangeDetached, origin: "one"})
		events.expect(t, session.SessionDetached{Session: "s", Role: session.Participant, Origin: "one"})
		next.Detach()
		changes.expect(t, expected{kind: session.ChangeControlChanged})
		events.expect(t, session.ControlChanged{Session: "s"})
		// The consumer still attached is told control stands with nobody,
		// which is the change a holder leaving makes.
		if origin, held := two.control(t); origin != "" || held {
			t.Fatalf("a holder leaving left control with %q, held %v", origin, held)
		}
		changes.expect(t, expected{kind: session.ChangeDetached, origin: "three"})
		events.expect(t, session.SessionDetached{Session: "s", Role: session.Participant, Origin: "three"})

		// The machine's channel ending is the whole of what it says, with
		// the close it carried: the consumer still attached goes with the
		// session rather than detaching from it first.
		machine.close(t, duplex.CodePolicyViolation, "the machine went away")
		two.ended(t)
		changes.expect(t, expected{kind: session.ChangeUnbound})
		events.expect(t, session.SessionUnbound{Session: "s", Code: int(duplex.CodePolicyViolation), Reason: "the machine went away"})
		changes.quiet(t)
		events.quiet(t)
	})

	t.Run("a session is observed where its machine speaks, and through no consumer's connection", func(t *testing.T) {
		// A session emits where it stands, which is its own side: what a
		// consumer's connection runs over hears what that connection carries
		// and nothing of the session it is attached to.
		up, down := observing(), observing()
		registry := observedBy(session.Options{}, up)
		near, _ := connect(t, up)
		if err := registry.Bind("s", near, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		up.expect(t, session.SessionBound{Session: "s"})
		consumer, _ := connect(t, down)
		one, err := registry.Attach("s", consumer, session.Participant, "one", 0)
		if err != nil {
			t.Fatal(err)
		}
		up.expect(t, session.SessionAttached{Session: "s", Role: session.Participant, Origin: "one"})
		down.quiet(t)
		one.Detach()
		up.expect(t, session.SessionDetached{Session: "s", Role: session.Participant, Origin: "one"})
		down.quiet(t)
	})

	t.Run("no change and no event carries what was in a frame", func(t *testing.T) {
		// The sentinel is in every payload a session carries — a consumer's
		// params, the machine's result, an event's data, an ask's params, the
		// answer's result and a refused request's params — and in the meta a
		// frame carries, which is a carriage rather than a payload and may hold
		// a credential; it is in nothing any change or any event says.
		const sentinel = "sentinel-6f9c2a"
		events := observing()
		registry := observedBy(session.Options{}, events)
		changes := watching(t, registry, "s")
		near, far := connect(t, events)
		if err := registry.Bind("s", near, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		machine := newSpeaker("machine", far, false)
		holder, one := attach(t, registry, "s", "one", session.Participant, 0)
		_, watcher := attach(t, registry, "s", "watcher", session.Observer, 0)
		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		one.control(t)
		watcher.control(t)
		one.send(t, fmt.Sprintf(`{"version":1,"kind":"request","id":"c:1","method":"echo","params":{"text":%q,"count":1},"meta":{"secret":%q}}`, sentinel, sentinel))
		asked := machine.take(t)
		machine.send(t, fmt.Sprintf(`{"version":1,"kind":"response","id":%q,"result":{"text":%q,"count":1}}`, asked.text("id"), sentinel))
		one.take(t)
		machine.send(t, fmt.Sprintf(`{"version":1,"kind":"event","event":"changed","data":{"text":%q,"count":1},"meta":{"secret":%q}}`, sentinel, sentinel))
		one.take(t)
		watcher.take(t)
		machine.send(t, fmt.Sprintf(`{"version":1,"kind":"request","id":"s:1","method":"reverse","params":{"text":%q,"count":1}}`, sentinel))
		one.take(t)
		one.send(t, fmt.Sprintf(`{"version":1,"kind":"response","id":"s:1","result":{"text":%q,"count":1}}`, sentinel))
		machine.take(t)
		watcher.send(t, fmt.Sprintf(`{"version":1,"kind":"request","id":"c:1","method":"echo","params":{"text":%q,"count":1}}`, sentinel))
		watcher.alone(t)

		// What a change says is the members it declares, so a message
		// cannot arrive under a name the sentinel was not looked for under.
		members := []string{"At", "Session", "Kind", "Attachment", "Sequence", "Method", "Trace"}
		declared := reflect.TypeOf(session.Change{})
		if declared.NumField() != len(members) {
			t.Fatalf("a change declares %d members, not the %d the sentinel is looked for under", declared.NumField(), len(members))
		}
		for i, name := range members {
			if declared.Field(i).Name != name {
				t.Fatalf("a change's member %d is %s, not %s", i, declared.Field(i).Name, name)
			}
		}
		told := changes.all(t)
		if len(told) < 10 {
			t.Fatalf("the session told %d changes, which is not the scenario", len(told))
		}
		for _, change := range told {
			rendered := fmt.Sprintf("%+v %s %s %s %s %s", change, change.Kind, change.Session, change.Method,
				change.Trace.Parent, change.Trace.State)
			if change.Attachment != nil {
				rendered += " " + change.Attachment.Origin + " " + change.Attachment.Role.String()
			}
			if strings.Contains(rendered, sentinel) {
				t.Fatalf("a %s change carried what was in the frame: %s", change.Kind, rendered)
			}
		}
		// And the same of the events, rendered whole: every member of every
		// one of the ten, marshalled and printed.
		seen := events.all(t)
		if len(seen) != len(told) {
			t.Fatalf("the session told %d changes and %d events, which are one domain change said twice", len(told), len(seen))
		}
		for _, event := range seen {
			marshalled, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			rendered := fmt.Sprintf("%+v %s", event, marshalled)
			if strings.Contains(rendered, sentinel) {
				t.Fatalf("a %T event carried what was in the frame: %s", event, rendered)
			}
		}
	})

	t.Run("a request beyond what a session may have open is refused, and the refusal is a change like any other", func(t *testing.T) {
		registry := session.New(session.Options{MaxInflight: 1})
		changes := watching(t, registry, "s")
		near, far := connect(t, nil)
		if err := registry.Bind("s", near, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		machine := newSpeaker("machine", far, false)
		holder, one := attach(t, registry, "s", "one", session.Participant, 0)
		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		one.control(t)
		one.send(t, `{"version":1,"kind":"request","id":"c:1","method":"echo","params":{"text":"t","count":1}}`)
		machine.take(t)
		one.send(t, `{"version":1,"kind":"request","id":"c:2","method":"echo","params":{"text":"t","count":1}}`)
		refused := one.alone(t)
		var public struct{ Code string }
		if err := json.Unmarshal(refused.member["error"], &public); err != nil {
			t.Fatal(err)
		}
		if public.Code != session.ErrorBusy {
			t.Fatalf("a request beyond the session's own was refused with %q", public.Code)
		}
		machine.quiet(t)
		changes.expect(t, expected{kind: session.ChangeBound})
		changes.expect(t, expected{kind: session.ChangeAttached, origin: "one"})
		changes.expect(t, expected{kind: session.ChangeControlChanged, origin: "one"})
		changes.expect(t, expected{kind: session.ChangeFrameAppended, origin: "one", method: "echo", sequence: 1})
		changes.expect(t, expected{kind: session.ChangeRefused, origin: "one", method: "echo"})
		changes.quiet(t)
	})

	t.Run("stopping a registration stops it and no other", func(t *testing.T) {
		registry := session.New(session.Options{})
		kept := watching(t, registry, "s")
		stopped := watching(t, registry, "s")
		stopped.stop()
		near, _ := connect(t, nil)
		if err := registry.Bind("s", near, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		kept.expect(t, expected{kind: session.ChangeBound})
		stopped.quiet(t)
		// And one stopped twice is one registration, not a second one gone.
		kept.stop()
		kept.stop()
		elsewhere, _ := connect(t, nil)
		if err := registry.Bind("t", elsewhere, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		kept.quiet(t)
		stopped.quiet(t)
	})

	t.Run("a message over the log's bound is replayed truncated", func(t *testing.T) {
		log := session.NewMemoryLog(16)
		ctx := context.Background()
		small := []byte(`{"kind":"event"}`)
		large := []byte(`{"kind":"event","data":"beyond the bound"}`)
		for _, message := range [][]byte{small, large} {
			if _, err := log.Append(ctx, session.Frame{Direction: session.Down, Message: message}); err != nil {
				t.Fatal(err)
			}
		}
		var replayed []session.Frame
		if err := log.Replay(ctx, 0, func(frame session.Frame) error {
			replayed = append(replayed, frame)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(replayed) != 2 {
			t.Fatalf("the log replayed %d frames", len(replayed))
		}
		if replayed[0].Truncated || string(replayed[0].Message) != string(small) || replayed[0].Sequence != 1 {
			t.Fatalf("a message within the bound was replayed as %+v", replayed[0])
		}
		if !replayed[1].Truncated || string(replayed[1].Message) != string(large[:16]) || replayed[1].Sequence != 2 {
			t.Fatalf("a message over the bound was replayed as %+v", replayed[1])
		}
		if err := log.Replay(ctx, 1, func(frame session.Frame) error {
			if frame.Sequence != 2 {
				t.Errorf("a replay after the first frame gave %d", frame.Sequence)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("every consumer is told who holds control, and one attaching is told before anything else", func(t *testing.T) {
		registry, machine := bind(t, "s")
		holder, one := attach(t, registry, "s", "one", session.Participant, 0)
		_, two := attach(t, registry, "s", "two", session.Observer, 0)
		// A consumer that joins a session nobody holds is told exactly that,
		// which is what a holder absent means and what an origin alone could
		// not say of a consumer attached under no name.
		for _, consumer := range []*speaker{one, two} {
			if consumer.holder != "" || consumer.held {
				t.Fatalf("%s joined a session held by %q", consumer.name, consumer.holder)
			}
		}
		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		for _, consumer := range []*speaker{one, two} {
			if origin, held := consumer.control(t); origin != "one" || !held {
				t.Fatalf("%s was told control is %q, held %v", consumer.name, origin, held)
			}
		}
		if err := registry.Control("s", nil); err != nil {
			t.Fatal(err)
		}
		for _, consumer := range []*speaker{one, two} {
			if origin, held := consumer.control(t); origin != "" || held {
				t.Fatalf("%s was told control released is %q, held %v", consumer.name, origin, held)
			}
		}
		// Given again, so that a consumer attaching after it joins a session
		// somebody holds, and with one frame in the log so that its replay
		// is not empty: who holds control comes before that too.
		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		one.control(t)
		two.control(t)
		const emitted = `{"version":1,"kind":"event","event":"changed","data":{"text":"t","count":1}}`
		machine.send(t, emitted)
		one.take(t)
		two.take(t)
		_, three := attach(t, registry, "s", "three", session.Observer, 0)
		if three.holder != "one" || !three.held {
			t.Fatalf("a consumer attaching was told control is %q, held %v", three.holder, three.held)
		}
		if replayed := three.take(t); string(replayed.raw) != emitted {
			t.Fatalf("what followed was %s", replayed.raw)
		}
		three.quiet(t)
	})

	t.Run("an attachment says what the relay told its consumer", func(t *testing.T) {
		registry, machine := bind(t, "s")
		first, one := attach(t, registry, "s", "one", session.Participant, 0)
		if origin, held := first.Holder(); origin != "" || held {
			t.Fatalf("an attachment of a session nobody holds says %q, held %v", origin, held)
		}
		if first.Sequence() != 0 {
			t.Fatalf("an attachment delivered nothing stands at %d", first.Sequence())
		}
		moved := make(chan string, 4)
		stop := first.OnControl(func(origin string, held bool) {
			if !held {
				moved <- "nobody"
				return
			}
			moved <- origin
		})
		if err := registry.Control("s", first); err != nil {
			t.Fatal(err)
		}
		one.control(t)
		if told := <-moved; told != "one" {
			t.Fatalf("the registration was told %q", told)
		}
		if origin, held := first.Holder(); origin != "one" || !held {
			t.Fatalf("the attachment says %q, held %v, where its consumer was told one", origin, held)
		}
		machine.send(t, `{"version":1,"kind":"event","event":"changed","data":{"text":"t","count":1}}`)
		one.take(t)
		if first.Sequence() != one.cursor || first.Sequence() != 1 {
			t.Fatalf("the attachment stands at %d where its consumer was told %d", first.Sequence(), one.cursor)
		}
		// A stopped registration hears nothing of what the consumer is still
		// told, and the state stands whether anything was registered at all.
		stop()
		if err := registry.Control("s", nil); err != nil {
			t.Fatal(err)
		}
		one.control(t)
		if origin, held := first.Holder(); origin != "" || held {
			t.Fatalf("the attachment says %q, held %v, where control was released", origin, held)
		}
		select {
		case told := <-moved:
			t.Fatalf("a stopped registration was told %q", told)
		default:
		}
	})

	t.Run("a consumer reattaches after the cursor it was told, which counting what arrived gets wrong", func(t *testing.T) {
		// A log bound with four frames in it, the second of them over its
		// bound and so kept cut: a cut message is no message for a channel
		// that speaks the family, so three arrive and the session stands at
		// four. A consumer counting what arrived would reattach at three and
		// be given the fourth a second time.
		held := []string{
			`{"version":1,"kind":"event","event":"changed","data":1}`,
			`{"version":1,"kind":"event","event":"changed","data":{"text":"well beyond the bound this log was given","count":2}}`,
			`{"version":1,"kind":"event","event":"changed","data":3}`,
			`{"version":1,"kind":"event","event":"changed","data":4}`,
		}
		log := session.NewMemoryLog(64)
		for _, message := range held {
			if _, err := log.Append(context.Background(), session.Frame{Direction: session.Down, Message: []byte(message)}); err != nil {
				t.Fatal(err)
			}
		}
		registry := session.New(session.Options{})
		near, far := connect(t, nil)
		if err := registry.Bind("s", near, governance, log); err != nil {
			t.Fatal(err)
		}
		machine := newSpeaker("machine", far, false)
		// A consumer holding the whole log, which is replayed nothing and is
		// where the suite reads that a live frame has been recorded.
		_, watcher := attach(t, registry, "s", "watcher", session.Observer, int64(len(held)))
		watcher.quiet(t)

		attachment, one := attach(t, registry, "s", "one", session.Observer, 0)
		for _, want := range []string{held[0], held[2], held[3]} {
			if replayed := one.take(t); string(replayed.raw) != want {
				t.Fatalf("the replay gave %s, not %s", replayed.raw, want)
			}
		}
		one.quiet(t)
		if one.cursor != 4 || attachment.Sequence() != 4 {
			t.Fatalf("a consumer given three of four frames was told it stands at %d, and its attachment says %d",
				one.cursor, attachment.Sequence())
		}
		// Nothing of the session's own vocabulary is in the log, so a replay
		// never gives a stale holder or a cursor of its own: what a consumer
		// is told is the relay's, made where it is sent.
		if err := log.Replay(context.Background(), 0, func(frame session.Frame) error {
			var named struct{ Event string }
			if json.Unmarshal(frame.Message, &named) == nil && strings.HasPrefix(named.Event, session.Prefix) {
				t.Errorf("the log kept %s", frame.Message)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		attachment.Detach()
		const live = `{"version":1,"kind":"event","event":"changed","data":5}`
		machine.send(t, live)
		watcher.take(t)
		_, again := attach(t, registry, "s", "one", session.Observer, one.cursor)
		if replayed := again.take(t); string(replayed.raw) != live {
			t.Fatalf("a consumer resuming after the cursor it was told saw %s", replayed.raw)
		}
		again.quiet(t)
		// And the count a consumer would have kept itself is one short.
		_, counted := attach(t, registry, "s", "counted", session.Observer, one.cursor-1)
		if replayed := counted.take(t); string(replayed.raw) != held[3] {
			t.Fatalf("a consumer resuming after what it counted saw %s", replayed.raw)
		}
	})

	t.Run("a machine that sends the session's own vocabulary ends the session", func(t *testing.T) {
		// The vocabulary is the relay's to produce: a machine speaking it
		// speaks for the layer above it, which is no frame of the family.
		for _, sent := range []string{
			`{"version":1,"kind":"event","event":"session.control","data":{"holder":"one"}}`,
			`{"version":1,"kind":"event","event":"session.cursor","data":{"sequence":9}}`,
			`{"version":1,"kind":"request","id":"s:1","method":"session.subscribe","params":{"events":[]}}`,
		} {
			registry, machine := bind(t, "s")
			_, one := attach(t, registry, "s", "one", session.Participant, 0)
			machine.send(t, sent)
			named := "session.control"
			switch {
			case strings.Contains(sent, session.CursorEvent):
				named = session.CursorEvent
			case strings.Contains(sent, "session.subscribe"):
				named = "session.subscribe"
			}
			// The consumer is ended with the session and was given nothing of
			// what the machine sent, which the log did not keep either.
			closed := one.ended(t)
			if closed.Code != duplex.CodeProtocolError || !strings.Contains(closed.Reason, named) {
				t.Fatalf("%s ended the consumer as %d %q", named, closed.Code, closed.Reason)
			}
			if ended := machine.ended(t); ended.Code != duplex.CodeProtocolError {
				t.Fatalf("%s ended the machine's own side as %d %q", named, ended.Code, ended.Reason)
			}
		}
	})

	t.Run("every refusal carries the code the session's vocabulary names", func(t *testing.T) {
		// What a call refuses with is the API a consumer's server is written
		// against, so it is a code a program branches on and not prose a
		// program would have to match. Both of the ways Go asks reach it.
		refused := func(t *testing.T, what, want string, err error) {
			t.Helper()
			if err == nil {
				t.Fatalf("%s was not refused", what)
			}
			var refusal *session.Error
			if !errors.As(err, &refusal) {
				t.Fatalf("%s was refused with %v, which errors.As does not reach as a *session.Error", what, err)
			}
			if refusal.Code != want {
				t.Fatalf("%s was refused with %q, wanted %q", what, refusal.Code, want)
			}
			if !errors.Is(err, &session.Error{Code: want}) {
				t.Fatalf("errors.Is does not reach %s by its code alone", what)
			}
			if refusal.Message == "" {
				t.Fatalf("%s was refused with a code and nothing for a person to read", what)
			}
		}

		registry, _ := bind(t, "s")
		spare := func(t *testing.T) duplex.Conn {
			t.Helper()
			near, _ := connect(t, nil)
			return near
		}

		// A session is bound under an id, over a connection, with the
		// family's governance and a log — four ways of the same refusal.
		refused(t, "a bind under no id", session.ErrorSessionInvalid,
			registry.Bind("", spare(t), governance, session.NewMemoryLog(0)))
		refused(t, "a bind over no connection", session.ErrorSessionInvalid,
			registry.Bind("t", nil, governance, session.NewMemoryLog(0)))
		refused(t, "a bind with no governance", session.ErrorSessionInvalid,
			registry.Bind("t", spare(t), session.Governance{}, session.NewMemoryLog(0)))
		refused(t, "a bind with no log", session.ErrorSessionInvalid,
			registry.Bind("t", spare(t), governance, nil))
		refused(t, "a bind under an id already bound", session.ErrorSessionExists,
			registry.Bind("s", spare(t), governance, session.NewMemoryLog(0)))

		// No session under that id, whichever call names it.
		_, err := registry.Attach("nothing", spare(t), session.Participant, "one", 0)
		refused(t, "an attach to a session nothing bound", session.ErrorNoSession, err)
		refused(t, "control of a session nothing bound", session.ErrorNoSession, registry.Control("nothing", nil))

		// What a consumer attaches with: a connection, a role, a sequence.
		_, err = registry.Attach("s", nil, session.Participant, "one", 0)
		refused(t, "an attach over no connection", session.ErrorSessionInvalid, err)
		_, err = registry.Attach("s", spare(t), session.Role(-1), "one", 0)
		refused(t, "an attach in a role that is not one", session.ErrorRoleInvalid, err)
		_, err = registry.Attach("s", spare(t), session.Participant, "one", -1)
		refused(t, "an attach after what is no sequence", session.ErrorSequenceInvalid, err)

		// Who may be given control: a consumer of this session, and a
		// participant.
		one, _ := attach(t, registry, "s", "one", session.Participant, 0)
		watcher, _ := attach(t, registry, "s", "watcher", session.Observer, 0)
		refused(t, "control given to an observer", session.ErrorNotControlling, registry.Control("s", watcher))
		elsewhere, _ := bind(t, "elsewhere")
		other, _ := attach(t, elsewhere, "elsewhere", "other", session.Participant, 0)
		refused(t, "control given to a consumer of another session", session.ErrorNotAttached, registry.Control("s", other))
		one.Detach()
		refused(t, "control given to a consumer that has left", session.ErrorNotAttached, registry.Control("s", one))

		// No room for another consumer.
		full := session.New(session.Options{MaxAttachments: 1})
		near, far := connect(t, nil)
		_ = newSpeaker("machine", far, false)
		if err := full.Bind("s", near, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		attach(t, full, "s", "first", session.Participant, 0)
		_, err = full.Attach("s", spare(t), session.Participant, "second", 0)
		refused(t, "an attach beyond what the session holds", session.ErrorTooManyAttachments, err)

		// And what the package's own log refuses: a replay with nowhere to
		// deliver.
		refused(t, "a replay with nowhere to deliver", session.ErrorInvalidOptions,
			session.NewMemoryLog(0).Replay(context.Background(), 0, nil))
	})

	t.Run("a frame that is not text ends the connection as unsupported data", func(t *testing.T) {
		// A frame of the wrong kind carries no message of the profile at
		// all, which is unsupported data — 1003 — where text that is no
		// message of it is a fault of another kind and closes with another
		// code. Either side may send one and each is refused the same way.
		const said = "a session speaks JSON text frames"
		registry, machine := bind(t, "s")
		_, one := attach(t, registry, "s", "one", session.Participant, 0)
		_, two := attach(t, registry, "s", "watcher", session.Observer, 0)

		one.sendBinary(t, []byte{0x00, 0x01})
		if closed := one.ended(t); closed.Code != duplex.CodeUnsupportedData || closed.Reason != said {
			t.Fatalf("a consumer's binary frame ended its connection as %d %q", closed.Code, closed.Reason)
		}
		// The session stands and goes on routing: only the consumer that
		// sent it is gone.
		machine.send(t, `{"version":1,"kind":"event","event":"changed","data":{"text":"still here","count":1}}`)
		if got := two.take(t); got.text("event") != "changed" {
			t.Fatalf("the session stopped routing after a consumer's binary frame: %s", got.raw)
		}

		// The machine's own ends the session, and every consumer with it,
		// under the same code and the same reason.
		machine.sendBinary(t, []byte{0x02})
		if ended := two.ended(t); ended.Code != duplex.CodeUnsupportedData || ended.Reason != said {
			t.Fatalf("the machine's binary frame ended a consumer as %d %q", ended.Code, ended.Reason)
		}
		if up := machine.ended(t); up.Code != duplex.CodeUnsupportedData || up.Reason != said {
			t.Fatalf("the machine's own side ended as %d %q", up.Code, up.Reason)
		}
	})

	t.Run("text that is no message of the profile ends the connection as a protocol error", func(t *testing.T) {
		// Text is where a message of the profile would be, so what is wrong
		// is the message and not the frame: a protocol error — 1002 — under a
		// reason naming the fault, where a frame of the wrong kind is
		// unsupported data. The reasons are the closed set the decoder gives
		// and are the same in both languages, a consumer reading one off the
		// close being unable to ask which runtime wrote the relay — an object
		// malformed inside among them, where a decoder's own words for a
		// syntax error would be one runtime's prose on the wire.
		for _, malformed := range []struct{ what, sent, said string }{
			{"text that is no JSON at all", `not json`, "a session frame must be a JSON object"},
			{"a JSON array", `["version",1]`, "a session frame must be a JSON object"},
			{"an object malformed inside", `{"version":1,"kind":}`, "a session frame must be a JSON object"},
			{"a member named twice", `{"version":1,"kind":"request","id":"c:1","id":"c:2","method":"no_args","params":{}}`, `duplicate session frame member "id"`},
			{"content after the object", `{"version":1,"kind":"event","event":"changed","data":{"text":"x","count":1}} {}`, "invalid trailing session frame content"},
		} {
			t.Run(malformed.what, func(t *testing.T) {
				registry, machine := bind(t, "s")
				_, one := attach(t, registry, "s", "one", session.Participant, 0)
				_, two := attach(t, registry, "s", "watcher", session.Observer, 0)

				one.send(t, malformed.sent)
				if closed := one.ended(t); closed.Code != duplex.CodeProtocolError || closed.Reason != malformed.said {
					t.Fatalf("a consumer sending %s ended as %d %q, where 1002 %q was due", malformed.what, closed.Code, closed.Reason, malformed.said)
				}
				// The session stands and goes on routing: only the consumer
				// that sent it is gone.
				machine.send(t, `{"version":1,"kind":"event","event":"changed","data":{"text":"still here","count":1}}`)
				if got := two.take(t); got.text("event") != "changed" {
					t.Fatalf("the session stopped routing after a consumer sent %s: %s", malformed.what, got.raw)
				}

				// The machine's own ends the session, and every consumer with
				// it, under the same code and the same reason.
				machine.send(t, malformed.sent)
				if ended := two.ended(t); ended.Code != duplex.CodeProtocolError || ended.Reason != malformed.said {
					t.Fatalf("the machine sending %s ended a consumer as %d %q, where 1002 %q was due", malformed.what, ended.Code, ended.Reason, malformed.said)
				}
				if up := machine.ended(t); up.Code != duplex.CodeProtocolError || up.Reason != malformed.said {
					t.Fatalf("the machine's own side ended as %d %q, where 1002 %q was due", up.Code, up.Reason, malformed.said)
				}
			})
		}
	})
}

// The trace contexts the change run sends, each on a frame of its own, so
// that a change saying which frame it concerns says which of them it was.
// The relay reads none of them and neither does the suite: they are three
// W3C strings carried through and read back.
const (
	firstTrace  = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	askTrace    = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	answerTrace = "00-0af7651916cd43dd8448eb211c80319c-00f067aa0ba902b8-01"
)

// watch is one OnChange registration the suite reads as a stream of what
// the registry told it, in the order it was told.
type watch struct {
	session string
	changes chan session.Change
	stop    func()
}

// watching registers before anything is bound, so that a session's first
// change is one of the ones the suite reads.
func watching(t *testing.T, registry *session.Registry, id string) *watch {
	t.Helper()
	w := &watch{session: id, changes: make(chan session.Change, 256)}
	w.stop = registry.OnChange(func(change session.Change) { w.changes <- change })
	t.Cleanup(w.stop)
	return w
}

// expected is one change as the suite expects it: what happened, to which
// consumer where there is one, what the frame it concerns named, where the
// session's log stood, and the trace that frame carried.
type expected struct {
	kind     session.ChangeKind
	origin   string
	method   string
	sequence int64
	trace    string
}

func (w *watch) expect(t *testing.T, want expected) session.Change {
	t.Helper()
	select {
	case got := <-w.changes:
		origin := ""
		if got.Attachment != nil {
			origin = got.Attachment.Origin
		}
		if got.Kind != want.kind || origin != want.origin || got.Method != want.method ||
			got.Sequence != want.sequence || got.Trace.Parent != want.trace {
			t.Fatalf("the session changed %s(consumer %q, method %q, sequence %d, trace %q), not %s(consumer %q, method %q, sequence %d, trace %q)",
				got.Kind, origin, got.Method, got.Sequence, got.Trace.Parent,
				want.kind, want.origin, want.method, want.sequence, want.trace)
		}
		if got.Session != w.session {
			t.Fatalf("a %s change of session %q reached a watcher of %q", got.Kind, got.Session, w.session)
		}
		if got.At.IsZero() {
			t.Fatalf("a %s change happened at no time", got.Kind)
		}
		return got
	case <-time.After(5 * time.Second):
		t.Fatalf("nothing changed where %s was expected", want.kind)
	}
	return session.Change{}
}

// quiet holds that the session changed no further: what is refused, routed
// elsewhere or told to another registration reaches this one nowhere.
func (w *watch) quiet(t *testing.T) {
	t.Helper()
	select {
	case got := <-w.changes:
		t.Fatalf("the session also changed %s", got.Kind)
	case <-time.After(250 * time.Millisecond):
	}
}

// all is every change told so far, once nothing more is coming.
func (w *watch) all(t *testing.T) []session.Change {
	t.Helper()
	var told []session.Change
	for {
		select {
		case got := <-w.changes:
			told = append(told, got)
		case <-time.After(250 * time.Millisecond):
			return told
		}
	}
}

// listening is what the peer a session's machine speaks over tells, kept as
// the stream of this session's own events: the runtime's and the tunnel's
// pass through it, because one observer hears every layer and the suite is
// holding this one.
type listening struct {
	events chan runtime.ObserverEvent
}

func observing() *listening { return &listening{events: make(chan runtime.ObserverEvent, 256)} }

func (l *listening) Observe(event runtime.ObserverEvent) {
	switch event.(type) {
	case session.SessionBound, session.SessionUnbound, session.SessionAttached, session.SessionDetached,
		session.AskRaised, session.AskRouted, session.AskAnswered, session.ControlChanged,
		session.FrameAppended, session.Refused:
		l.events <- event
	}
}

// expect holds the next event to one written out whole but for its At, which
// is read off it and held to having happened: an event the suite does not
// spell every member of is an event whose members nobody is holding.
func (l *listening) expect(t *testing.T, want runtime.ObserverEvent) runtime.ObserverEvent {
	t.Helper()
	select {
	case got := <-l.events:
		if reflect.TypeOf(got) != reflect.TypeOf(want) {
			t.Fatalf("the observer saw a %T, not a %T", got, want)
		}
		whole := reflect.New(reflect.TypeOf(got)).Elem()
		whole.Set(reflect.ValueOf(got))
		at, ok := whole.FieldByName("At").Interface().(time.Time)
		if !ok || at.IsZero() {
			t.Fatalf("a %T happened at no time", got)
		}
		whole.FieldByName("At").Set(reflect.ValueOf(time.Time{}))
		if !reflect.DeepEqual(whole.Interface(), want) {
			t.Fatalf("the observer saw %+v, not %+v", whole.Interface(), want)
		}
		return got
	case <-time.After(5 * time.Second):
		t.Fatalf("the observer saw nothing where a %T was expected", want)
	}
	return nil
}

// quiet holds that the session told the observer nothing further.
func (l *listening) quiet(t *testing.T) {
	t.Helper()
	select {
	case got := <-l.events:
		t.Fatalf("the observer also saw %+v", got)
	case <-time.After(250 * time.Millisecond):
	}
}

// all is every event told so far, once nothing more is coming.
func (l *listening) all(t *testing.T) []runtime.ObserverEvent {
	t.Helper()
	var told []runtime.ObserverEvent
	for {
		select {
		case got := <-l.events:
			told = append(told, got)
		case <-time.After(250 * time.Millisecond):
			return told
		}
	}
}

// intact holds every member of a frame but its id to what was sent, in the
// order it was sent: the relay rewrites the id and forwards the rest.
func intact(t *testing.T, sent string, received *frame) {
	t.Helper()
	original, err := members(t, []byte(sent))
	if err != nil {
		t.Fatal(err)
	}
	if len(original.order) != len(received.order) {
		t.Fatalf("%s arrived as %s", sent, received.raw)
	}
	for i, name := range original.order {
		if received.order[i] != name {
			t.Fatalf("member %d arrived as %q, not %q", i, received.order[i], name)
		}
		if name == "id" {
			continue
		}
		if string(received.member[name]) != string(original.member[name]) {
			t.Fatalf("member %q arrived as %s, not %s", name, received.member[name], original.member[name])
		}
	}
}

// mustConnection takes the registry's end of a fresh pair.
func mustConnection(t *testing.T, connect Connect) duplex.Conn {
	t.Helper()
	near, _ := connect(t, nil)
	return near
}

// speaker is one end of a connection the suite speaks frames over: a
// machine, or a consumer. A consumer also reads the session's own
// vocabulary, and keeps the last cursor it was told, which is what it
// reattaches after.
//
// It reads its end as the frames arrive rather than as the suite asks for
// them, so that nothing the relay sends waits on the suite's own turn to
// read: a replay is longer than a connection's buffer, and the attach that
// carries it does not return until the last of it is sent.
type speaker struct {
	name    string
	channel duplex.Conn
	// consumer says this end is a consumer's, which is the end a cursor
	// reaches: the machine's is one side of the family's conversation and
	// hears nothing of the session's own vocabulary.
	consumer bool
	cursor   int64
	// holder and held are what the last control event this end was sent
	// said, which is how a case reads the one the attach took.
	holder string
	held   bool
	frames chan *frame
	ending chan error
}

// newSpeaker takes one end and begins reading it.
func newSpeaker(name string, channel duplex.Conn, consumer bool) *speaker {
	s := &speaker{name: name, channel: channel, consumer: consumer,
		frames: make(chan *frame, 256), ending: make(chan error, 1)}
	go s.pump()
	return s
}

// pump reads until the end closes, and says how it ended; it holds no t, a
// test's own goroutine being the only one that may fail it.
func (s *speaker) pump() {
	for {
		received, err := s.channel.Receive(context.Background())
		if err != nil {
			s.ending <- err
			close(s.frames)
			return
		}
		decoded, err := parseFrame(received.Data)
		if err != nil {
			s.ending <- fmt.Errorf("%s received %q: %w", s.name, received.Data, err)
			close(s.frames)
			return
		}
		s.frames <- decoded
	}
}

// frame is one frame as it arrived, with its members in the order they were
// written.
type frame struct {
	raw    []byte
	order  []string
	member map[string]json.RawMessage
}

func (f *frame) text(name string) string {
	var value string
	if json.Unmarshal(f.member[name], &value) != nil {
		return ""
	}
	return value
}

// members reads a frame's members in the order they were written, as the
// relay does, so that the suite can hold a forwarded frame to the one it
// was sent.
func members(t *testing.T, raw []byte) (*frame, error) {
	t.Helper()
	return parseFrame(raw)
}

// parseFrame is that read without a test to fail, which is what a speaker's
// own goroutine does with what arrives on it.
func parseFrame(raw []byte) (*frame, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, errors.New("a frame must be a JSON object")
	}
	decoded := &frame{raw: raw, member: map[string]json.RawMessage{}}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, err
		}
		name, ok := key.(string)
		if !ok {
			return nil, errors.New("invalid frame member name")
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, err
		}
		decoded.order = append(decoded.order, name)
		decoded.member[name] = value
	}
	return decoded, nil
}

func (s *speaker) send(t *testing.T, raw string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.channel.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: []byte(raw)}); err != nil {
		t.Fatalf("%s could not send: %v", s.name, err)
	}
}

// sendBinary sends a frame of the other kind, which a session speaks none
// of: what it is refused with is the case below.
func (s *speaker) sendBinary(t *testing.T, data []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.channel.Send(ctx, duplex.Frame{Kind: duplex.Binary, Data: data}); err != nil {
		t.Fatalf("%s could not send: %v", s.name, err)
	}
}

// take is the next frame of the family and, where this end is a consumer's,
// the cursor the relay sends straight after it: the two go under one turn,
// so a consumer reads them together, and what the cursor said is where this
// speaker now stands.
func (s *speaker) take(t *testing.T) *frame {
	t.Helper()
	got := s.alone(t)
	if s.consumer {
		s.cursor = s.at(t, got)
	}
	return got
}

// alone is the next frame with nothing after it: what the relay writes of
// itself — a refusal, an ask handed again as control moves — has no place in
// the log and so no cursor.
func (s *speaker) alone(t *testing.T) *frame {
	t.Helper()
	select {
	case got, ok := <-s.frames:
		if !ok {
			t.Fatalf("%s's channel ended where a frame was due: %v", s.name, <-s.ending)
		}
		return got
	case <-time.After(5 * time.Second):
		t.Fatalf("%s received nothing", s.name)
	}
	return nil
}

// stands is a cursor with nothing before it: what a replay ends with where
// the frames it passed over are its last, so that a consumer resuming from
// it reads the log on rather than over them again.
func (s *speaker) stands(t *testing.T) int64 {
	t.Helper()
	s.cursor = s.at(t, nil)
	return s.cursor
}

// at holds that a cursor followed the frame just taken and gives the
// sequence it named, which is the log's own and never a count of frames.
// after is the frame it names, and nil where the cursor stands alone.
func (s *speaker) at(t *testing.T, after *frame) int64 {
	t.Helper()
	cursor := s.alone(t)
	if cursor.text("kind") != "event" || cursor.text("event") != session.CursorEvent {
		named := "the replay's end"
		if after != nil {
			named = string(after.raw)
		}
		t.Fatalf("%s was sent %s after %s, where the cursor was due", s.name, cursor.raw, named)
	}
	var data struct{ Sequence int64 }
	if err := json.Unmarshal(cursor.member["data"], &data); err != nil {
		t.Fatal(err)
	}
	if data.Sequence <= 0 {
		t.Fatalf("%s was told it stands at %d", s.name, data.Sequence)
	}
	return data.Sequence
}

// control is the next frame, held to being the session's control event, and
// who it says holds control.
func (s *speaker) control(t *testing.T) (origin string, held bool) {
	t.Helper()
	got := s.alone(t)
	if got.text("kind") != "event" || got.text("event") != session.ControlEvent {
		t.Fatalf("%s was sent %s, where who holds control was due", s.name, got.raw)
	}
	var data struct{ Holder *string }
	if err := json.Unmarshal(got.member["data"], &data); err != nil {
		t.Fatal(err)
	}
	if data.Holder != nil {
		s.holder, s.held = *data.Holder, true
	} else {
		s.holder, s.held = "", false
	}
	return s.holder, s.held
}

// joined is the control event every consumer is sent on attach, before its
// replay and before any frame of the family.
func (s *speaker) joined(t *testing.T) (origin string, held bool) {
	t.Helper()
	return s.control(t)
}

// quiet holds that nothing reaches this end: what the relay refuses, or
// routes elsewhere, arrives nowhere. An end that has closed is quiet as one
// that sends nothing is.
func (s *speaker) quiet(t *testing.T) {
	t.Helper()
	select {
	case got, ok := <-s.frames:
		if ok {
			t.Fatalf("%s received %q", s.name, got.raw)
		}
	case <-time.After(250 * time.Millisecond):
	}
}

func (s *speaker) close(t *testing.T, code duplex.Code, reason string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.channel.Close(ctx, code, reason); err != nil {
		t.Fatalf("%s could not close: %v", s.name, err)
	}
}

// ended waits for this end's channel to be closed and returns the close.
func (s *speaker) ended(t *testing.T) *duplex.CloseError {
	t.Helper()
	select {
	case got, ok := <-s.frames:
		if ok {
			t.Fatalf("%s received %q where its channel should have ended", s.name, got.raw)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s's channel never ended", s.name)
	}
	var closed *duplex.CloseError
	if err := <-s.ending; !errors.As(err, &closed) {
		t.Fatalf("%s ended with %v", s.name, err)
	}
	return closed
}

// Probe is the generator's probe family as a session governs by it, read
// from the corpus by the tool's loader: the suite is held to what the
// corpus declares and never to a copy of it.
func Probe(t *testing.T) session.Governance {
	t.Helper()
	root := checkout(t)
	family, problems := load.Family(os.DirFS(root), "cmd/nightseam/testdata/corpus/api/contracts/probe", "probe", nil)
	if len(problems) != 0 {
		t.Fatalf("the corpus probe family: %v", problems)
	}
	if family == nil || family.Session == nil {
		t.Fatal("the corpus probe family declares no session tier")
	}
	governance := session.Governance{Decides: names(family.Session.Decides), Asks: names(family.Session.Asks)}
	if !governance.Decides("echo") || governance.Decides("no_args") || !governance.Asks("reverse") {
		t.Fatalf("the corpus probe family governs %v and %v", family.Session.Decides, family.Session.Asks)
	}
	return governance
}

func names(declared []string) func(string) bool {
	set := make(map[string]bool, len(declared))
	for _, name := range declared {
		set[name] = true
	}
	return func(method string) bool { return set[method] }
}

// checkout is the nearest ancestor of the test's directory that holds
// go.mod, so moving the package does not move the corpus the suite governs
// by.
func checkout(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}
