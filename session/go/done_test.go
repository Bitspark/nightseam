package session_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/session/go"
)

type stalledSend struct {
	duplex.Conn
	stall atomic.Bool
}

func (c *stalledSend) Send(ctx context.Context, frame duplex.Frame) error {
	if !c.stall.Load() {
		return c.Conn.Send(ctx, frame)
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestAttachmentDone(t *testing.T) {
	for _, ending := range []string{"detach", "consumer close", "session end", "protocol error", "send timeout"} {
		t.Run(ending, func(t *testing.T) {
			registry := session.New(session.Options{SendTimeout: 20 * time.Millisecond})
			up, machine := duplex.Pipe(1 << 20)
			t.Cleanup(func() { _ = machine.Abort() })
			governance := session.Governance{Decides: func(string) bool { return false }, Asks: func(string) bool { return false }}
			if err := registry.Bind("s", up, governance, session.NewMemoryLog(0)); err != nil {
				t.Fatal(err)
			}
			near, consumer := duplex.Pipe(1 << 20)
			t.Cleanup(func() { _ = consumer.Abort() })
			down := &stalledSend{Conn: near}
			attachment, err := registry.Attach("s", down, session.Participant, "one", 0)
			if err != nil {
				t.Fatal(err)
			}
			done := attachment.Done()
			select {
			case <-done:
				t.Fatal("a live attachment is done")
			default:
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			switch ending {
			case "detach":
				attachment.Detach()
			case "consumer close":
				err = consumer.Close(ctx, duplex.CodeNormal, "left")
			case "session end":
				err = machine.Close(ctx, duplex.CodeNormal, "finished")
			case "protocol error":
				err = consumer.Send(ctx, duplex.Frame{Kind: duplex.Text, Data: []byte("not JSON")})
			case "send timeout":
				down.stall.Store(true)
				err = machine.Send(ctx, duplex.Frame{Kind: duplex.Text,
					Data: []byte(`{"version":1,"kind":"event","event":"changed","data":{}}`)})
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("attachment termination did not close Done")
			}
			attachment.Detach()
			if attachment.Done() != done {
				t.Fatal("Done changed channels")
			}
			select {
			case <-attachment.Done():
			default:
				t.Fatal("a late observer missed termination")
			}
		})
	}
}
