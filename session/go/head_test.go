package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/session/go"
)

type countedHeadLog struct {
	session.Log
	head    int64
	err     error
	heads   int
	replays int
}

func (l *countedHeadLog) Head(context.Context) (int64, error) {
	l.heads++
	return l.head, l.err
}

func (l *countedHeadLog) Replay(ctx context.Context, after int64, deliver func(session.Frame) error) error {
	l.replays++
	return l.Log.Replay(ctx, after, deliver)
}

func TestBindPrefersHead(t *testing.T) {
	ctx := context.Background()
	beneath := session.NewMemoryLog(0)
	for i := 0; i < 3; i++ {
		if _, err := beneath.Append(ctx, session.Frame{Direction: session.Down,
			Message: []byte(`{"version":1,"kind":"event","event":"changed","data":{}}`)}); err != nil {
			t.Fatal(err)
		}
	}
	log := &countedHeadLog{Log: beneath, head: 3}
	registry, err := session.New(session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	up, machine := duplex.Pipe(1 << 20)
	t.Cleanup(func() { _ = machine.Abort() })
	governance := session.Governance{Decides: func(string) bool { return false }, Asks: func(string) bool { return false }}
	if err := registry.Bind("s", up, governance, log); err != nil {
		t.Fatal(err)
	}
	if log.heads != 1 || log.replays != 0 {
		t.Fatalf("bind called Head %d times and Replay %d times", log.heads, log.replays)
	}
	down, consumer := duplex.Pipe(1 << 20)
	t.Cleanup(func() { _ = consumer.Abort() })
	attachment, err := registry.Attach("s", down, session.Observer, "one", 0)
	if err != nil {
		t.Fatal(err)
	}
	if attachment.Sequence() != 3 || log.replays != 1 {
		t.Fatalf("attach stood at %d after %d replays", attachment.Sequence(), log.replays)
	}
}

func TestBindRefusesAnUnavailableHead(t *testing.T) {
	for _, tc := range []struct {
		name string
		head int64
		err  error
		want string
	}{
		{"error", 0, errors.New("head unavailable"), "head unavailable"},
		{"negative", -1, nil, "negative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &countedHeadLog{Log: session.NewMemoryLog(0), head: tc.head, err: tc.err}
			registry, err := session.New(session.Options{})
			if err != nil {
				t.Fatal(err)
			}
			up, machine := duplex.Pipe(1 << 20)
			t.Cleanup(func() { _ = machine.Abort() })
			governance := session.Governance{Decides: func(string) bool { return false }, Asks: func(string) bool { return false }}
			err = registry.Bind("s", up, governance, log)
			if !errors.Is(err, &session.Error{Code: session.ErrorSessionInvalid}) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("bind returned %v", err)
			}
			if log.replays != 0 {
				t.Fatal("a failed head lookup fell back to replay")
			}
			if err := registry.Control("s", nil); !errors.Is(err, &session.Error{Code: session.ErrorNoSession}) {
				t.Fatalf("failed binding left a session: %v", err)
			}
		})
	}
}

func TestMemoryLogHead(t *testing.T) {
	log := session.NewMemoryLog(0)
	header, ok := log.(interface {
		Head(context.Context) (int64, error)
	})
	if !ok {
		t.Fatal("the memory log cannot report its head")
	}
	for want := int64(0); want < 4; want++ {
		if got, err := header.Head(context.Background()); err != nil || got != want {
			t.Fatalf("Head = %d, %v; want %d", got, err, want)
		}
		if _, err := log.Append(context.Background(), session.Frame{}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := header.Head(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Head on a cancelled context returned %v", err)
	}
}
