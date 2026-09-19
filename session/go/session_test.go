package session_test

import (
	"context"
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
func channels(t *testing.T) (near, far *tunnel.Channel) {
	t.Helper()
	return observed(t, nil)
}

// observed is one connected pair of channels, each of a tunnel of its own
// over a pipe: the transport beneath a session, as the suite asks for it.
// The near end's peer takes the observer, because that is the end a registry
// binds and so the peer a session of it emits its events through.
func observed(t *testing.T, observer runtime.Observer) (near, far *tunnel.Channel) {
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
	far = <-accepted
	if far == nil {
		t.Fatal("nobody accepted the channel")
	}
	return opened, far
}

// TestSessionOverPipes holds this package to the relay's contract.
func TestSessionOverPipes(t *testing.T) {
	sessiontest.Run(t, observed)
}

// TestBindTakesASessionOnce: a session is bound under an id, over a
// channel, with the family's governance and a log, and only once.
func TestBindTakesASessionOnce(t *testing.T) {
	registry := session.New(session.Options{})
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
	registry := session.New(session.Options{})
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
	registry := session.New(session.Options{MaxAttachments: 1, SendTimeout: time.Second})
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
	registry := session.New(session.Options{})
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
