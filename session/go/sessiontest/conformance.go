// Package sessiontest holds a session component to what a relay promises.
// An implementation's own tests call Run with a way to make a connected
// pair of channels; what Run checks is the routing table of the boundary
// rule and nothing of the transport beneath it, so a consumer written
// against the component can trust the same things wherever it runs: a
// response reaches the one consumer that asked, an event reaches every
// consumer, control decides what a consumer may send, an ask follows
// control, ids are the session's own, a consumer resumes from the log, and
// a channel ending ends what it should and nothing more.
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
	"github.com/Bitspark/nightseam/tunnel/go"
)

// Connect makes one connected pair of channels of the probe family: near is
// the end a registry binds or attaches, far the end a machine or a consumer
// speaks over. The peer near runs over is given observer, which is nil where
// the suite is not watching — a session emits its events through the peer of
// the channel its machine speaks on, so that is the end an observer goes on.
// The suite closes what it opened; the transport may register cleanup with t.
type Connect func(t *testing.T, observer runtime.Observer) (near, far *tunnel.Channel)

// Run holds a session component to the relay's contract.
func Run(t *testing.T, connect Connect) {
	t.Helper()
	governance := Probe(t)

	// bind makes a registry with one session bound, and returns the machine's
	// end of its channel.
	bind := func(t *testing.T, id string) (*session.Registry, *speaker) {
		t.Helper()
		registry := session.New(session.Options{})
		near, far := connect(t, nil)
		if err := registry.Bind(id, near, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		return registry, &speaker{name: "machine", channel: far}
	}
	attach := func(t *testing.T, registry *session.Registry, id, origin string, role session.Role, after int64) (*session.Attachment, *speaker) {
		t.Helper()
		near, far := connect(t, nil)
		attachment, err := registry.Attach(id, near, role, origin, after)
		if err != nil {
			t.Fatal(err)
		}
		return attachment, &speaker{name: origin, channel: far}
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
			refused := consumer.take(t)
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
		machine.send(t, `{"version":1,"kind":"request","id":"s:1","method":"reverse","params":{"text":"t","count":1}}`)
		if ask := one.take(t); ask.text("method") != "reverse" {
			t.Fatalf("the holder saw %s", ask.raw)
		}
		two.quiet(t)
		if err := registry.Control("s", second); err != nil {
			t.Fatal(err)
		}
		if ask := two.take(t); ask.text("id") != "s:1" || ask.text("method") != "reverse" {
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
		// Three frames are in the log: the request as the machine saw it, its
		// response, and the event.
		_, late := attach(t, registry, "s", "late", session.Observer, 0)
		replayed := []*frame{late.take(t), late.take(t), late.take(t)}
		if replayed[0].text("method") != "no_args" || replayed[0].text("id") != asked.text("id") {
			t.Fatalf("the replay began with %s", replayed[0].raw)
		}
		if replayed[1].text("kind") != "response" || replayed[2].text("event") != "changed" {
			t.Fatalf("the replay went %s then %s", replayed[1].raw, replayed[2].raw)
		}
		machine.send(t, `{"version":1,"kind":"event","event":"changed","data":{"text":"two","count":2}}`)
		if live := late.take(t); string(live.member["data"]) != `{"text":"two","count":2}` {
			t.Fatalf("the live frame after the replay was %s", live.raw)
		}
		// A consumer that holds the first two frames resumes at the third.
		_, later := attach(t, registry, "s", "later", session.Observer, 2)
		if resumed := later.take(t); resumed.text("event") != "changed" || string(resumed.member["data"]) != `{"text":"one","count":1}` {
			t.Fatalf("a consumer resuming after two frames saw %s", resumed.raw)
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
		machine := &speaker{name: "machine", channel: far}
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
		if _, err := registry.Attach("s", mustChannel(t, connect), session.Participant, "late", 0); err == nil {
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
			machines[id] = &speaker{name: "machine " + id, channel: far}
			down, consumer := connect(t, nil)
			attachment, err := registry.Attach(id, down, session.Participant, id, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := registry.Control(id, attachment); err != nil {
				t.Fatal(err)
			}
			holders[id] = &speaker{name: id, channel: consumer}
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

	t.Run("every domain change of a session reaches OnChange and the peer's observer, in the order the registry made them", func(t *testing.T) {
		// One session from bound to unbound, read twice over: the change the
		// registry told and the event the observer of the peer the machine's
		// channel runs over was given, which are one domain change said
		// twice. The scenarios above, in one run.
		registry := session.New(session.Options{})
		changes := watching(t, registry, "s")
		events := observing()
		near, far := connect(t, events)
		if err := registry.Bind("s", near, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		machine := &speaker{name: "machine", channel: far}
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
		two.take(t)
		changes.expect(t, expected{kind: session.ChangeRefused, origin: "two", method: "echo"})
		events.expect(t, session.Refused{Session: "s", Code: session.ErrorNotControlling, Method: "echo",
			Role: session.Observer, Origin: "two"})

		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		changes.expect(t, expected{kind: session.ChangeControlChanged, origin: "one"})
		events.expect(t, session.ControlChanged{Session: "s", Origin: "one", Held: true})

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

		// A consumer resuming from nothing takes the eight frames from the
		// log, which is no change of the session's: a replay is what one
		// consumer is told, not something that happened to the session.
		next, three := attach(t, registry, "s", "three", session.Participant, 0)
		changes.expect(t, expected{kind: session.ChangeAttached, origin: "three"})
		events.expect(t, session.SessionAttached{Session: "s", Role: session.Participant, Origin: "three"})
		for i := 0; i < 8; i++ {
			three.take(t)
		}

		// Control moving asks the open request afresh of whoever holds it
		// now, and both are changes.
		if err := registry.Control("s", next); err != nil {
			t.Fatal(err)
		}
		changes.expect(t, expected{kind: session.ChangeControlChanged, origin: "three"})
		events.expect(t, session.ControlChanged{Session: "s", Origin: "three", Held: true})
		three.take(t)
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

	t.Run("a session is observed through the peer its machine speaks over, and through no consumer's", func(t *testing.T) {
		// A session emits where it stands, which is its own side: the peer a
		// consumer's channel runs over hears what that peer carries and
		// nothing of the session the channel is attached to.
		registry := session.New(session.Options{})
		up, down := observing(), observing()
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
		// answer's result and a refused request's params — and in nothing any
		// change or any event says.
		const sentinel = "sentinel-6f9c2a"
		registry := session.New(session.Options{})
		changes := watching(t, registry, "s")
		events := observing()
		near, far := connect(t, events)
		if err := registry.Bind("s", near, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		machine := &speaker{name: "machine", channel: far}
		holder, one := attach(t, registry, "s", "one", session.Participant, 0)
		_, watcher := attach(t, registry, "s", "watcher", session.Observer, 0)
		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		one.send(t, fmt.Sprintf(`{"version":1,"kind":"request","id":"c:1","method":"echo","params":{"text":%q,"count":1}}`, sentinel))
		asked := machine.take(t)
		machine.send(t, fmt.Sprintf(`{"version":1,"kind":"response","id":%q,"result":{"text":%q,"count":1}}`, asked.text("id"), sentinel))
		one.take(t)
		machine.send(t, fmt.Sprintf(`{"version":1,"kind":"event","event":"changed","data":{"text":%q,"count":1}}`, sentinel))
		one.take(t)
		watcher.take(t)
		machine.send(t, fmt.Sprintf(`{"version":1,"kind":"request","id":"s:1","method":"reverse","params":{"text":%q,"count":1}}`, sentinel))
		one.take(t)
		one.send(t, fmt.Sprintf(`{"version":1,"kind":"response","id":"s:1","result":{"text":%q,"count":1}}`, sentinel))
		machine.take(t)
		watcher.send(t, fmt.Sprintf(`{"version":1,"kind":"request","id":"c:1","method":"echo","params":{"text":%q,"count":1}}`, sentinel))
		watcher.take(t)

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
		machine := &speaker{name: "machine", channel: far}
		holder, one := attach(t, registry, "s", "one", session.Participant, 0)
		if err := registry.Control("s", holder); err != nil {
			t.Fatal(err)
		}
		one.send(t, `{"version":1,"kind":"request","id":"c:1","method":"echo","params":{"text":"t","count":1}}`)
		machine.take(t)
		one.send(t, `{"version":1,"kind":"request","id":"c:2","method":"echo","params":{"text":"t","count":1}}`)
		refused := one.take(t)
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

// mustChannel takes the registry's end of a fresh pair.
func mustChannel(t *testing.T, connect Connect) *tunnel.Channel {
	t.Helper()
	near, _ := connect(t, nil)
	return near
}

// speaker is one end of a channel the suite speaks frames over: a machine,
// or a consumer.
type speaker struct {
	name    string
	channel *tunnel.Channel
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

func (s *speaker) take(t *testing.T) *frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	received, err := s.channel.Receive(ctx)
	if err != nil {
		t.Fatalf("%s received nothing: %v", s.name, err)
	}
	decoded, err := members(t, received.Data)
	if err != nil {
		t.Fatalf("%s received %q: %v", s.name, received.Data, err)
	}
	return decoded
}

// quiet holds that nothing reaches this end: what the relay refuses, or
// routes elsewhere, arrives nowhere.
func (s *speaker) quiet(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	received, err := s.channel.Receive(ctx)
	if err == nil {
		t.Fatalf("%s received %q", s.name, received.Data)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	received, err := s.channel.Receive(ctx)
	if err == nil {
		t.Fatalf("%s received %q where its channel should have ended", s.name, received.Data)
	}
	var closed *duplex.CloseError
	if !errors.As(err, &closed) {
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
