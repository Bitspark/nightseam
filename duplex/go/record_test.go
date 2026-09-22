package duplex_test

import (
	"context"
	"encoding/json"
	"errors"
	bitwire "github.com/Bitspark/bitwire/wire/go"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
)

type recordTarget struct {
	values chan duplex.WireRecord
	closed chan struct{}
	once   sync.Once
	refuse error
}

func newRecordTarget() *recordTarget {
	return &recordTarget{values: make(chan duplex.WireRecord, 64), closed: make(chan struct{})}
}
func (w *recordTarget) Send(p []string, m bitwire.Message) error {
	if w.refuse != nil {
		return w.refuse
	}
	select {
	case <-w.closed:
		return duplex.ErrClosed
	default:
	}
	w.values <- duplex.WireRecord{Path: p, Message: m}
	return nil
}

type brokenSequenceLog struct {
	*duplex.MemoryWireLog
	appendGap bool
}

func (l *brokenSequenceLog) Append(ctx context.Context, p []string, m bitwire.Message) (uint64, error) {
	seq, err := l.MemoryWireLog.Append(ctx, p, m)
	if l.appendGap {
		seq++
	}
	return seq, err
}
func (l *brokenSequenceLog) Read(ctx context.Context, seq uint64) (duplex.WireRecord, error) {
	entry, err := l.MemoryWireLog.Read(ctx, seq)
	entry.Sequence++
	return entry, err
}

func TestRecordedWireStorageAndTargetFailures(t *testing.T) {
	for _, mode := range []string{"append", "sequence", "target"} {
		t.Run(mode, func(t *testing.T) {
			ctx := recordContext(t)
			sentinel := errors.New("consumer failure")
			target := newRecordTarget()
			var store duplex.WireLog = duplex.NewMemoryWireLog()
			if mode == "append" {
				held := newHeldLog()
				held.holdAppend = true
				held.failure = sentinel
				close(held.release)
				store = held
			}
			if mode == "sequence" {
				store = &brokenSequenceLog{MemoryWireLog: duplex.NewMemoryWireLog(), appendGap: true}
			}
			if mode == "target" {
				target.refuse = sentinel
			}
			ended := make(chan error, 1)
			var w *duplex.RecordedWire
			w, err := duplex.Record(ctx, target, store, duplex.RecordOptions{OnClose: func(err error) {
				_ = w.Close(1000, "reentrant")
				_, headErr := w.Head(ctx)
				if !errors.Is(headErr, duplex.ErrClosed) {
					ended <- headErr
					return
				}
				ended <- err
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := w.Send(nil, recordMessage(1)); err != nil {
				t.Fatal(err)
			}
			err = recordWait(t, ctx, ended)
			want := sentinel
			if mode == "sequence" {
				want = duplex.ErrRecordSequence
			}
			if !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
			select {
			case <-target.closed:
				t.Fatal("recorder closed its borrowed target")
			default:
			}
		})
	}
	for _, mode := range []string{"read", "sequence", "target"} {
		t.Run("follower "+mode, func(t *testing.T) {
			ctx := recordContext(t)
			sentinel := errors.New("replay failure")
			memory := duplex.NewMemoryWireLog()
			memory.Append(ctx, nil, recordMessage(1))
			var store duplex.WireLog = memory
			if mode == "read" {
				held := newHeldLog()
				held.MemoryWireLog = memory
				held.failure = sentinel
				close(held.release)
				store = held
			}
			if mode == "sequence" {
				store = &brokenSequenceLog{MemoryWireLog: memory}
			}
			w, err := duplex.Record(ctx, newRecordTarget(), store, duplex.RecordOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close(1000, "done")
			target := newRecordTarget()
			if mode == "target" {
				target.refuse = sentinel
			}
			f, err := w.Follow(ctx, 0, target)
			if err != nil {
				t.Fatal(err)
			}
			recordWait(t, ctx, f.Done)
			want := sentinel
			if mode == "sequence" {
				want = duplex.ErrRecordSequence
			}
			if !errors.Is(f.Err(), want) {
				t.Fatalf("got %v want %v", f.Err(), want)
			}
			if head, err := w.Head(ctx); err != nil || head != 1 {
				t.Fatalf("recorder affected: %d %v", head, err)
			}
		})
	}
}
func (*recordTarget) Receive(bitwire.Receiver) (func(), error) { return func() {}, nil }
func (w *recordTarget) Close(bitwire.Code, string) error {
	w.once.Do(func() { close(w.closed) })
	return nil
}
func recordMessage(i int) bitwire.Message {
	b, _ := json.Marshal(i)
	return bitwire.Message{Frame: bitwire.ProfileFrame{Version: 1, Kind: bitwire.ProfileEvent, Data: b}}
}
func recordContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func recordWait[T any](t *testing.T, ctx context.Context, c <-chan T) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		var z T
		return z
	}
}

type heldLog struct {
	*duplex.MemoryWireLog
	entered    chan struct{}
	release    chan struct{}
	once       sync.Once
	holdAppend bool
	failure    error
}

func newHeldLog() *heldLog {
	return &heldLog{MemoryWireLog: duplex.NewMemoryWireLog(), entered: make(chan struct{}), release: make(chan struct{})}
}

type heldHeadLog struct {
	*duplex.MemoryWireLog
	entered chan struct{}
}

func (l *heldHeadLog) Head(ctx context.Context) (uint64, error) {
	close(l.entered)
	<-ctx.Done()
	return 0, ctx.Err()
}
func TestRecordedWireSetupCanBeCancelled(t *testing.T) {
	ctx := recordContext(t)
	setup, cancel := context.WithCancel(ctx)
	store := &heldHeadLog{MemoryWireLog: duplex.NewMemoryWireLog(), entered: make(chan struct{})}
	target := newRecordTarget()
	done := make(chan error, 1)
	go func() { _, err := duplex.Record(setup, target, store, duplex.RecordOptions{}); done <- err }()
	recordWait(t, ctx, store.entered)
	cancel()
	if err := recordWait(t, ctx, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := target.Send(nil, recordMessage(1)); err != nil {
		t.Fatal("failed setup took ownership of target", err)
	}
}
func (l *heldLog) hold(ctx context.Context) error {
	l.once.Do(func() { close(l.entered) })
	select {
	case <-l.release:
		return l.failure
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (l *heldLog) Read(ctx context.Context, seq uint64) (duplex.WireRecord, error) {
	if !l.holdAppend {
		if err := l.hold(ctx); err != nil {
			return duplex.WireRecord{}, err
		}
	}
	return l.MemoryWireLog.Read(ctx, seq)
}
func (l *heldLog) Append(ctx context.Context, p []string, m bitwire.Message) (uint64, error) {
	if l.holdAppend {
		if err := l.hold(ctx); err != nil {
			return 0, err
		}
	}
	return l.MemoryWireLog.Append(ctx, p, m)
}

func TestRecordedWireHeadAndHandoff(t *testing.T) {
	var table struct {
		Cases []struct {
			Name           string
			Before, During []int
			After, Head    uint64
			Expected       []int
		}
	}
	b, err := os.ReadFile("../../conformance/tables/recorded-wire.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &table); err != nil {
		t.Fatal(err)
	}
	for _, c := range table.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ctx := recordContext(t)
			store := newHeldLog()
			original := newRecordTarget()
			mounted := duplex.Mount(map[string]bitwire.Endpoint{"history": original})
			defer mounted.Close(1000, "done")
			w, err := duplex.Record(ctx, duplex.At(mounted, []string{"history"}), store, duplex.RecordOptions{MaxQueuedMessages: 8})
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close(1000, "done")
			source := duplex.At(w, nil)
			for _, v := range c.Before {
				if err := source.Send([]string{"tick"}, recordMessage(v)); err != nil {
					t.Fatal(err)
				}
			}
			target := newRecordTarget()
			f, err := w.Follow(ctx, c.After, target)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if f.Head != c.Head {
				t.Fatalf("head %d", f.Head)
			}
			recordWait(t, ctx, store.entered)
			for _, v := range c.During {
				if err := source.Send([]string{"tick"}, recordMessage(v)); err != nil {
					t.Fatal(err)
				}
			}
			head, err := w.Head(ctx)
			if err != nil || head != uint64(len(c.Before)+len(c.During)) {
				t.Fatalf("producer cannot advance: %d %v", head, err)
			}
			close(store.release)
			var got []int
			for range c.Expected {
				e := recordWait(t, ctx, target.values)
				if !reflect.DeepEqual(e.Path, []string{"tick"}) {
					t.Fatal(e.Path)
				}
				var v int
				json.Unmarshal(e.Message.Frame.Data, &v)
				got = append(got, v)
			}
			if !reflect.DeepEqual(got, c.Expected) {
				t.Fatalf("got %v want %v", got, c.Expected)
			}
			if err := w.Send([]string{"fence"}, recordMessage(99)); err != nil {
				t.Fatal(err)
			}
			e := recordWait(t, ctx, target.values)
			if string(e.Message.Frame.Data) != "99" {
				t.Fatalf("duplicate before fence: %s", e.Message.Frame.Data)
			}
		})
	}
}

func TestRecordedWireStalledFollowerIsIsolated(t *testing.T) {
	ctx := recordContext(t)
	store := newHeldLog()
	original := newRecordTarget()
	w, err := duplex.Record(ctx, original, store, duplex.RecordOptions{MaxQueuedMessages: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close(1000, "done")
	w.Send([]string{"tick"}, recordMessage(1))
	slowTarget := newRecordTarget()
	mounted := duplex.Mount(map[string]bitwire.Endpoint{"slow": slowTarget})
	defer mounted.Close(1000, "done")
	slow, err := w.Follow(ctx, 0, duplex.At(mounted, []string{"slow"}))
	if err != nil {
		t.Fatal(err)
	}
	recordWait(t, ctx, store.entered)
	fastTarget := newRecordTarget()
	fast, err := w.Follow(ctx, 1, fastTarget)
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()
	for i := 2; i <= 4; i++ {
		if err := w.Send([]string{"tick"}, recordMessage(i)); err != nil {
			t.Fatal(err)
		}
		recordWait(t, ctx, fastTarget.values)
	}
	recordWait(t, ctx, slow.Done)
	if !errors.Is(slow.Err(), duplex.ErrRecordOverflow) {
		t.Fatal(slow.Err())
	}
	// The follower borrows send authority; both mount and target stay usable.
	if err := slowTarget.Send([]string{"probe"}, recordMessage(99)); err != nil {
		t.Fatal(err)
	}
	if err := w.Send([]string{"tick"}, recordMessage(5)); err != nil {
		t.Fatal(err)
	}
	recordWait(t, ctx, fastTarget.values)
}

func TestRecordedWireSlowStorageBoundsAdmission(t *testing.T) {
	ctx := recordContext(t)
	store := newHeldLog()
	store.holdAppend = true
	target := newRecordTarget()
	ended := make(chan error, 1)
	w, err := duplex.Record(ctx, target, store, duplex.RecordOptions{MaxQueuedMessages: 2, OnClose: func(err error) { ended <- err }})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close(1000, "done")
	w.Send(nil, recordMessage(1))
	recordWait(t, ctx, store.entered)
	w.Send(nil, recordMessage(2))
	w.Send(nil, recordMessage(3))
	if err := w.Send(nil, recordMessage(4)); !errors.Is(err, duplex.ErrRecordOverflow) {
		t.Fatal(err)
	}
	if err := recordWait(t, ctx, ended); !errors.Is(err, duplex.ErrRecordOverflow) {
		t.Fatal(err)
	}
	select {
	case <-target.closed:
		t.Fatal("recorder closed its borrowed target")
	default:
	}
}

func TestRecordedWireCursorCancellationAndCapabilities(t *testing.T) {
	ctx := recordContext(t)
	store := duplex.NewMemoryWireLog()
	target := newRecordTarget()
	w, err := duplex.Record(ctx, target, store, duplex.RecordOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close(1000, "done")
	if _, err := w.Follow(ctx, 1, newRecordTarget()); !errors.Is(err, duplex.ErrRecordSequence) {
		t.Fatal(err)
	}
	address := &bitwire.ReturnAddress{Wire: target}
	message := recordMessage(1)
	message.Return = address
	w.Send([]string{"", "é"}, message)
	followerTarget := newRecordTarget()
	life, cancel := context.WithCancel(ctx)
	f, err := w.Follow(life, 0, followerTarget)
	if err != nil {
		t.Fatal(err)
	}
	e := recordWait(t, ctx, followerTarget.values)
	if e.Message.Return != address {
		t.Fatal("return capability changed")
	}
	cancel()
	recordWait(t, ctx, f.Done)
	if !errors.Is(f.Err(), context.Canceled) {
		t.Fatal(f.Err())
	}
	f.Close()
	f.Close()
	if _, err := store.Read(ctx, 0); !errors.Is(err, duplex.ErrRecordSequence) {
		t.Fatal(err)
	}
}
