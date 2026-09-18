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
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/internal/load"
	"github.com/Bitspark/nightseam/session/go"
	"github.com/Bitspark/nightseam/tunnel/go"
)

// Connect makes one connected pair of channels of the probe family: near is
// the end a registry binds or attaches, far the end a machine or a consumer
// speaks over. The suite closes what it opened; the transport may register
// cleanup with t.
type Connect func(t *testing.T) (near, far *tunnel.Channel)

// Run holds a session component to the relay's contract.
func Run(t *testing.T, connect Connect) {
	t.Helper()
	governance := Probe(t)

	// bind makes a registry with one session bound, and returns the machine's
	// end of its channel.
	bind := func(t *testing.T, id string) (*session.Registry, *speaker) {
		t.Helper()
		registry := session.New(session.Options{})
		near, far := connect(t)
		if err := registry.Bind(id, near, governance, session.NewMemoryLog(0)); err != nil {
			t.Fatal(err)
		}
		return registry, &speaker{name: "machine", channel: far}
	}
	attach := func(t *testing.T, registry *session.Registry, id, origin string, role session.Role, after int64) (*session.Attachment, *speaker) {
		t.Helper()
		near, far := connect(t)
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
			near, far := connect(t)
			if err := registry.Bind(id, near, governance, session.NewMemoryLog(0)); err != nil {
				t.Fatal(err)
			}
			machines[id] = &speaker{name: "machine " + id, channel: far}
			down, consumer := connect(t)
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
	near, _ := connect(t)
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
