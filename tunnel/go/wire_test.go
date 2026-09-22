package tunnel_test

import (
	"context"
	"encoding/json"
	bitwire "github.com/Bitspark/bitwire/wire/go"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/tunnel/go"
)

type wireOpens struct{ count atomic.Int32 }

func (w *wireOpens) Observe(event runtime.ObserverEvent) {
	if _, ok := event.(runtime.ConnectionOpened); ok {
		w.count.Add(1)
	}
}

func TestChannelIsAPreparedWireBeforeSelection(t *testing.T) {
	client, server := peers(t, tunnel.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observer := &wireOpens{}
	acquired, stopAcquisition := context.WithCancel(ctx)
	digest := strings.Repeat("a", 64)
	opened, err := client.Open(acquired, "wire", digest, runtime.Options{Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	stopAcquisition()
	// The first request can already be waiting in the raw channel when accepted.
	result := make(chan error, 1)
	go func() {
		var got string
		err := runtime.CallWire(ctx, opened, []string{"deep", "echo"}, "ok", &got)
		if err == nil && got != "ok" {
			t.Errorf("got %q", got)
		}
		result <- err
	}()
	accepted, err := server.Accept(ctx, runtime.Options{Observer: observer, Prepare: func(peer *runtime.Peer) error {
		binding, err := runtime.NewDispatcher(peer.Wire())
		if err != nil {
			return err
		}
		_, err = runtime.HandleWire(binding, []string{"deep", "echo"}, func(_ context.Context, raw json.RawMessage) (any, error) { return raw, nil })
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if opened.Digest != digest || accepted.Digest != digest {
		t.Fatal("prepared channel lost declaration digest")
	}
	var _ bitwire.Wire = (*tunnel.Channel)(nil)
	if observer.count.Load() != 2 {
		t.Fatalf("construction peers %d", observer.count.Load())
	}
	selected := duplex.At(duplex.Mount(map[string]bitwire.Endpoint{"route": opened}), []string{"route", "deep"})
	var got string
	if err := runtime.CallWire(ctx, selected, []string{"echo"}, "selected", &got); err != nil || got != "selected" {
		t.Fatalf("selected %q %v", got, err)
	}
	cached, ok, err := client.Channel(opened.ID, runtime.Options{Prepare: func(*runtime.Peer) error { t.Fatal("lookup rebuilt peer"); return nil }})
	if err != nil || !ok || cached != opened || observer.count.Load() != 2 {
		t.Fatal("selection or lookup allocated a peer", err)
	}
	if _, ok := client.Connection(opened.ID); ok {
		t.Fatal("wire channel also handed out raw reader")
	}
	if err := accepted.Close(duplex.CodeNormal, "done"); err != nil {
		t.Fatal(err)
	}
	if client.Peer().Err() != nil || server.Peer().Err() != nil {
		t.Fatal("channel close ended outer peer")
	}
}

func TestRawConnectionCannotAcquireASecondWireReader(t *testing.T) {
	client, _ := peers(t, tunnel.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := client.OpenConnection(ctx, "raw", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := client.Channel(raw.ID, runtime.Options{}); !ok || err == nil {
		t.Fatalf("raw claimed as wire: %t %v", ok, err)
	}
	if got, ok := client.Connection(raw.ID); !ok || got != raw {
		t.Fatal("raw lookup identity changed")
	}
}

func TestServerOpenedWireChannelUsesOnePreparedPeerOnEachSide(t *testing.T) {
	client, server := peers(t, tunnel.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	prepare := runtime.Options{Prepare: func(peer *runtime.Peer) error {
		binding, err := runtime.NewDispatcher(peer.Wire())
		if err != nil {
			return err
		}
		_, err = runtime.HandleWire(binding, []string{"echo"}, func(_ context.Context, raw json.RawMessage) (any, error) { return raw, nil })
		return err
	}}
	opened, err := server.Open(ctx, "reverse", "", prepare)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := client.Accept(ctx, prepare)
	if err != nil {
		t.Fatal(err)
	}
	if opened.ID%2 != 0 {
		t.Fatal("server changed channel parity")
	}
	for _, wire := range []bitwire.Wire{opened, accepted} {
		var result string
		if err := runtime.CallWire(ctx, wire, []string{"echo"}, "both ways", &result); err != nil || result != "both ways" {
			t.Fatalf("call %q %v", result, err)
		}
	}
}
