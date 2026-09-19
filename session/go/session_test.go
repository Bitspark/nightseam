package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/session/go"
	"github.com/Bitspark/nightseam/session/go/sessiontest"
	"github.com/Bitspark/nightseam/tunnel/go"
)

// channels is one connected pair of channels over peers nobody observes,
// which is what every test here but the suite's own asks for.
func channels(t *testing.T) (near, far duplex.Conn) {
	t.Helper()
	return observed(t, nil)
}

// observed is one connected pair of channels, each of a tunnel of its own
// over a pipe: the transport beneath a session, as the suite asks for it.
// The near end's peer takes the observer, because that is the end a registry
// binds and so the peer a session of it emits its events through.
func observed(t *testing.T, observer runtime.Observer) (near, far duplex.Conn) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a, b := duplex.Pipe(1 << 20)
	client, err := runtime.NewPeer(ctx, a, runtime.ClientRole, runtime.Options{Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	server, err := runtime.NewPeer(ctx, b, runtime.ServerRole, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	opening, err := tunnel.New(client, tunnel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	accepting, err := tunnel.New(server, tunnel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan *tunnel.Channel, 1)
	go func() {
		channel, err := accepting.Accept(ctx)
		if err != nil {
			t.Error(err)
		}
		accepted <- channel
	}()
	opened, err := opening.Open(ctx, "probe", 0)
	if err != nil {
		t.Fatal(err)
	}
	// Read as the channel it is rather than into far: an interface holding a
	// nil channel is not nil, so the one check worth making would not hold.
	channel := <-accepted
	if channel == nil {
		t.Fatal("nobody accepted the channel")
	}
	return opened, channel
}

// piped is one connected pair of the seam's own pipe: connections that run
// over no peer at all, which is what an in-process machine binds over and
// what a consumer over a bare socket attaches over. Nothing is seated with
// the observer here, a pipe having no peer to seat it on; the suite gives
// the registry the same one instead, which is the second of the two places
// a relay looks for it.
func piped(t *testing.T, _ runtime.Observer) (near, far duplex.Conn) {
	t.Helper()
	near, far = duplex.Pipe(1 << 20)
	t.Cleanup(func() { _ = near.Abort(); _ = far.Abort() })
	return near, far
}

// TestSessionOverChannels holds this package to the relay's contract over
// the channels of a tunnel, which is what a session ran over when it could
// run over nothing else.
func TestSessionOverChannels(t *testing.T) {
	sessiontest.Run(t, observed)
}

// TestSessionOverPipes holds it to the same contract over the seam's own
// pipe, with no tunnel anywhere: the relay sends on the connection, receives
// from it and closes it, and what multiplexed it — nothing, here — is none
// of its business.
func TestSessionOverPipes(t *testing.T) {
	sessiontest.Run(t, piped)
}

// TestBindTakesASessionOnce: a session is bound under an id, over a
// channel, with the family's governance and a log, and only once.
func TestBindTakesASessionOnce(t *testing.T) {
	registry, err := session.New(session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	governance := sessiontest.Probe(t)
	up, _ := channels(t)
	log := session.NewMemoryLog(0)
	for _, refused := range []struct {
		why string
		err error
	}{
		{"an id", registry.Bind("", up, governance, log)},
		{"a channel", registry.Bind("s", nil, governance, log)},
		{"governance", registry.Bind("s", up, session.Governance{}, log)},
		{"a log", registry.Bind("s", up, governance, nil)},
	} {
		if refused.err == nil {
			t.Errorf("a session was bound without %s", refused.why)
		}
	}
	if err := registry.Bind("s", up, governance, log); err != nil {
		t.Fatal(err)
	}
	other, _ := channels(t)
	if err := registry.Bind("s", other, governance, log); err == nil {
		t.Fatal("one id bound two sessions")
	}
	if attention := registry.Attention(); len(attention) != 0 {
		t.Fatalf("a bound session already wants attention: %v", attention)
	}
}

// TestAttachAndControlNameASession: a consumer attaches to a session that
// is bound, in a role the package knows, and control is one of that
// session's own attachments.
func TestAttachAndControlNameASession(t *testing.T) {
	registry, err := session.New(session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	governance := sessiontest.Probe(t)
	up, _ := channels(t)
	if err := registry.Bind("s", up, governance, session.NewMemoryLog(0)); err != nil {
		t.Fatal(err)
	}
	down, _ := channels(t)
	if _, err := registry.Attach("other", down, session.Participant, "one", 0); err == nil {
		t.Fatal("a consumer attached to a session nobody bound")
	}
	if _, err := registry.Attach("s", nil, session.Participant, "one", 0); err == nil {
		t.Fatal("a consumer attached over no channel")
	}
	if _, err := registry.Attach("s", down, session.Role(7), "one", 0); err == nil {
		t.Fatal("a consumer attached in a role the package does not know")
	}
	one, err := registry.Attach("s", down, session.Participant, "one", 0)
	if err != nil {
		t.Fatal(err)
	}
	if one.Role != session.Participant || one.Origin != "one" || one.Channel != down {
		t.Fatalf("the attachment is %+v", one)
	}
	if err := registry.Control("other", one); err == nil {
		t.Fatal("control was given on a session nobody bound")
	}
	// An attachment of another session is not this one's to give control to.
	elsewhere, _ := channels(t)
	if err := registry.Bind("t", elsewhere, governance, session.NewMemoryLog(0)); err != nil {
		t.Fatal(err)
	}
	stranger, _ := channels(t)
	foreign, err := registry.Attach("t", stranger, session.Participant, "stranger", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Control("s", foreign); err == nil || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("a stranger took control: %v", err)
	}
	if err := registry.Control("s", one); err != nil {
		t.Fatal(err)
	}
	// Detaching releases what it held, and the session goes on.
	one.Detach()
	one.Detach()
	if err := registry.Control("s", one); err == nil {
		t.Fatal("a detached consumer took control")
	}
	if err := registry.Control("s", nil); err != nil {
		t.Fatal(err)
	}
}

// TestAttachmentsAreBounded: a session takes as many consumers as its
// options allow and no more.
func TestAttachmentsAreBounded(t *testing.T) {
	registry, err := session.New(session.Options{MaxAttachments: 1, SendTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	up, _ := channels(t)
	if err := registry.Bind("s", up, sessiontest.Probe(t), session.NewMemoryLog(0)); err != nil {
		t.Fatal(err)
	}
	first, _ := channels(t)
	if _, err := registry.Attach("s", first, session.Participant, "one", 0); err != nil {
		t.Fatal(err)
	}
	second, _ := channels(t)
	if _, err := registry.Attach("s", second, session.Observer, "two", 0); err == nil {
		t.Fatal("a session took a consumer beyond its bound")
	}
}

// seating is a log whose first replay — the one Bind reads the head with —
// says that it has begun and waits to be let go, so that a test can have the
// machine speak while the cursor is being seated. Every later replay, which
// is a consumer's, runs as the log beneath it does.
type seating struct {
	session.Log
	began   chan struct{}
	release chan struct{}
	once    sync.Once
}

func (l *seating) Replay(ctx context.Context, after int64, deliver func(session.Frame) error) error {
	l.once.Do(func() {
		close(l.began)
		<-l.release
	})
	return l.Log.Replay(ctx, after, deliver)
}

// TestBindRecordsAboveTheHeadAFrameSentWhileItReads: the machine speaks
// while Bind is reading the log's head, and what it sent is recorded above
// that head rather than under a sequence the log has already given out —
// which is what seating the cursor before the pump, under the relay's lock,
// is for. Run under -race it is also the two goroutines on the cursor.
func TestBindRecordsAboveTheHeadAFrameSentWhileItReads(t *testing.T) {
	const held = 3
	beneath := session.NewMemoryLog(0)
	for i := 0; i < held; i++ {
		if _, err := beneath.Append(context.Background(), session.Frame{Direction: session.Down,
			Message: []byte(`{"version":1,"kind":"event","event":"changed","data":{"text":"held","count":1}}`)}); err != nil {
			t.Fatal(err)
		}
	}
	log := &seating{Log: beneath, began: make(chan struct{}), release: make(chan struct{})}
	registry, err := session.New(session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	appended := make(chan int64, 8)
	stop := registry.OnChange(func(change session.Change) {
		if change.Kind == session.ChangeFrameAppended {
			appended <- change.Sequence
		}
	})
	t.Cleanup(stop)
	up, far := channels(t)
	bound := make(chan error, 1)
	go func() { bound <- registry.Bind("s", up, sessiontest.Probe(t), log) }()
	<-log.began
	// The machine speaks with the read in progress; the frame waits on the
	// channel until the pump, which the seat runs before, reads it.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := far.Send(ctx, duplex.Frame{Kind: duplex.Text,
		Data: []byte(`{"version":1,"kind":"event","event":"changed","data":{"text":"live","count":2}}`)}); err != nil {
		t.Fatal(err)
	}
	close(log.release)
	if err := <-bound; err != nil {
		t.Fatal(err)
	}
	select {
	case sequence := <-appended:
		if sequence != held+1 {
			t.Fatalf("a frame sent while the cursor was seated was recorded at %d, not above the log's head of %d", sequence, held)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the frame the machine sent while the cursor was seated was never recorded")
	}
}

// watcher is an observer that keeps what it was told, so that a test can ask
// which of a session's events reached it.
type watcher struct {
	mu   sync.Mutex
	seen []string
}

func (w *watcher) Observe(event runtime.ObserverEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seen = append(w.seen, fmt.Sprintf("%T", event))
}

func (w *watcher) told() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.seen...)
}

// await waits for one event to have arrived, the last of a session's being
// told after the close that ends it.
func (w *watcher) await(t *testing.T, event string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, told := range w.told() {
			if told == event {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s never arrived; the observer was told %v", event, w.told())
}

// says sends one frame of the family over a connection, as a machine or a
// consumer of the suite would.
func says(t *testing.T, over duplex.Conn, message string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := over.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: []byte(message)}); err != nil {
		t.Fatal(err)
	}
}

// family takes the next frame of the family from a connection, passing over
// the session's own vocabulary — who holds control, where the cursor stands
// — which a consumer is told beside the conversation it is attached to.
func family(t *testing.T, over duplex.Conn) string {
	t.Helper()
	for {
		message := hears(t, over)
		var named struct {
			Event string `json:"event"`
		}
		if json.Unmarshal([]byte(message), &named) == nil && strings.HasPrefix(named.Event, session.Prefix) {
			continue
		}
		return message
	}
}

// ends waits for a connection to be closed and gives the close, passing
// over the session's own vocabulary as family does: a cursor the consumer
// never read is no frame of the conversation it was in.
func ends(t *testing.T, ctx context.Context, over duplex.Conn) *duplex.CloseError {
	t.Helper()
	for {
		received, err := over.Receive(ctx)
		if err != nil {
			var closed *duplex.CloseError
			if !errors.As(err, &closed) {
				t.Fatalf("the connection ended as %v", err)
			}
			return closed
		}
		var named struct {
			Event string `json:"event"`
		}
		if json.Unmarshal(received.Data, &named) != nil || !strings.HasPrefix(named.Event, session.Prefix) {
			t.Fatalf("the connection was left open, carrying %s", received.Data)
		}
	}
}

// hears takes the next frame of a connection as the text it carries.
func hears(t *testing.T, over duplex.Conn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	frame, err := over.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return string(frame.Data)
}

// drive takes one session of a registry from bound to unbound over pipes,
// through every event a session has: bound, attached twice, control moved,
// an ask raised, routed and answered, frames appended, a consumer refused,
// one detached and the session unbound.
func drive(t *testing.T, registry *session.Registry) {
	t.Helper()
	up, machine := duplex.Pipe(1 << 20)
	if err := registry.Bind("s", up, sessiontest.Probe(t), session.NewMemoryLog(0)); err != nil {
		t.Fatal(err)
	}
	near, holderEnd := duplex.Pipe(1 << 20)
	holder, err := registry.Attach("s", near, session.Participant, "one", 0)
	if err != nil {
		t.Fatal(err)
	}
	idle, idleEnd := duplex.Pipe(1 << 20)
	if _, err := registry.Attach("s", idle, session.Participant, "two", 0); err != nil {
		t.Fatal(err)
	}
	if err := registry.Control("s", holder); err != nil {
		t.Fatal(err)
	}
	says(t, machine, `{"version":1,"kind":"request","id":"s:1","method":"reverse","params":{"text":"t","count":1}}`)
	family(t, holderEnd)
	says(t, holderEnd, `{"version":1,"kind":"response","id":"s:1","result":{"text":"t","count":1}}`)
	hears(t, machine)
	// A participant that does not hold control is refused in the machine's
	// place, which the machine never sees and the observer does.
	says(t, idleEnd, `{"version":1,"kind":"request","id":"c:1","method":"echo","params":{"text":"t","count":1}}`)
	family(t, idleEnd)
	holder.Detach()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := machine.Close(ctx, duplex.CodeGoingAway, "the machine went away"); err != nil {
		t.Fatal(err)
	}
}

// TestASessionOverAConnectionWithNoPeerObservesThroughTheRegistry: a session
// bound over the seam's pipe has no peer to observe through, and tells the
// registry's observer the ten events it would have told a peer's.
func TestASessionOverAConnectionWithNoPeerObservesThroughTheRegistry(t *testing.T) {
	seen := &watcher{}
	registry, err := session.New(session.Options{Observer: seen, SendTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	drive(t, registry)
	seen.await(t, "session.SessionUnbound")
	told := map[string]bool{}
	for _, event := range seen.told() {
		told[event] = true
	}
	for _, event := range []string{
		"session.SessionBound", "session.SessionAttached", "session.ControlChanged",
		"session.AskRaised", "session.AskRouted", "session.AskAnswered",
		"session.FrameAppended", "session.Refused", "session.SessionDetached",
		"session.SessionUnbound",
	} {
		if !told[event] {
			t.Errorf("the registry's observer was never told %s; it heard %v", event, seen.told())
		}
	}
}

// TestASessionOverAConnectionWithNeitherObservesNothing: no peer and no
// registry observer is the no-op an observer already means, not a failure —
// the same session runs and nothing is told.
func TestASessionOverAConnectionWithNeitherObservesNothing(t *testing.T) {
	registry, err := session.New(session.Options{SendTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	drive(t, registry)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if len(registry.Attention()) == 0 && registry.Control("s", nil) != nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the session never ended")
}

// TestAMachineOverAPipeAndAConsumerOverAWebSocket is the mixed case the
// seam's connection makes possible: the machine is this process, bound over
// a pipe with no tunnel and no socket between it and the relay, and the
// consumer is elsewhere, attached over a channel of a tunnel over a real
// WebSocket. One session, two transports, and the relay asking neither what
// carried it: a call up, an event down, an ask answered, and the machine's
// end ending the consumer.
func TestAMachineOverAPipeAndAConsumerOverAWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	registry, err := session.New(session.Options{SendTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	// The machine: a peer of the family in this process, over a pipe it
	// speaks the profile on and nothing else.
	up, own := duplex.Pipe(1 << 20)
	machine, err := runtime.NewPeer(ctx, own, runtime.ServerRole, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer machine.Close()
	if err := machine.Handle("echo", func(_ context.Context, _ *runtime.Peer, params json.RawMessage) (any, error) {
		var said struct {
			Text  string `json:"text"`
			Count int    `json:"count"`
		}
		if err := json.Unmarshal(params, &said); err != nil {
			return nil, err
		}
		return map[string]any{"text": "machine:" + said.Text, "count": said.Count}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Bind("s", up, sessiontest.Probe(t), session.NewMemoryLog(0)); err != nil {
		t.Fatal(err)
	}

	// The consumer: a channel of a tunnel over a WebSocket, which the
	// service accepts and attaches to the same session.
	attached := make(chan *session.Attachment, 1)
	handler, err := runtime.NewHandler(runtime.ServerOptions{
		Authenticate: func(*http.Request) (context.Context, error) { return ctx, nil },
		CheckOrigin:  func(*http.Request) bool { return true },
		OnConnect: func(peer *runtime.Peer) {
			serving, err := tunnel.New(peer, tunnel.Options{})
			if err != nil {
				t.Error(err)
				return
			}
			channel, err := serving.Accept(peer.Context())
			if err != nil {
				t.Error(err)
				return
			}
			attachment, err := registry.Attach("s", channel, session.Participant, "consumer", 0)
			if err != nil {
				t.Error(err)
				return
			}
			attached <- attachment
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	peer, _, err := runtime.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), runtime.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	dialled, err := tunnel.New(peer, tunnel.Options{})
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := dialled.Open(ctx, "probe", 0)
	if err != nil {
		t.Fatal(err)
	}
	var attachment *session.Attachment
	select {
	case attachment = <-attached:
	case <-ctx.Done():
		t.Fatal("the consumer's channel was never attached")
	}
	if err := registry.Control("s", attachment); err != nil {
		t.Fatal(err)
	}

	// A call: the consumer's request reaches the machine under an id of the
	// session's own and its answer comes back under the consumer's.
	says(t, consumer, `{"version":1,"kind":"request","id":"c:1","method":"echo","params":{"text":"over the seam","count":1}}`)
	if answered := family(t, consumer); !strings.Contains(answered, `"id":"c:1"`) || !strings.Contains(answered, `machine:over the seam`) {
		t.Fatalf("the consumer saw %s", answered)
	}

	// An event: what the machine emits reaches every consumer attached.
	if err := machine.Emit(ctx, "changed", map[string]any{"text": "moved", "count": 2}); err != nil {
		t.Fatal(err)
	}
	if event := family(t, consumer); !strings.Contains(event, `"event":"changed"`) {
		t.Fatalf("the consumer saw %s", event)
	}

	// An ask: what the machine asks is routed to the holder of control, and
	// its answer travels back over the pipe.
	asked := make(chan error, 1)
	var reversed struct {
		Text  string `json:"text"`
		Count int    `json:"count"`
	}
	go func() {
		asked <- machine.Call(ctx, "reverse", map[string]any{"text": "deliver", "count": 1}, &reversed)
	}()
	ask := family(t, consumer)
	if !strings.Contains(ask, `"method":"reverse"`) {
		t.Fatalf("the holder saw %s", ask)
	}
	var carried struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(ask), &carried); err != nil {
		t.Fatal(err)
	}
	says(t, consumer, fmt.Sprintf(`{"version":1,"kind":"response","id":%q,"result":{"text":"reveiled","count":1}}`, carried.ID))
	if err := <-asked; err != nil {
		t.Fatal(err)
	}
	if reversed.Text != "reveiled" {
		t.Fatalf("the machine was answered %+v", reversed)
	}

	// The close: the machine's connection ending ends the consumer's
	// channel, under the close the machine's carried — a machine that stops
	// by choice is the normal close it chose, where one whose transport was
	// dropped would be the abnormal closure the seam calls 1006.
	machine.Close()
	if closed := ends(t, ctx, consumer); closed.Code != duplex.CodeNormal {
		t.Fatalf("the consumer's channel ended as %v", closed)
	}
}
