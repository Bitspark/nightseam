package duplex_test

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	bitwire "github.com/Bitspark/bitwire/wire/go"
	"github.com/Bitspark/nightseam/duplex/go"
)

// throughOrigin is a borrowed endpoint that counts its attachments.
type throughOrigin struct {
	mu        sync.Mutex
	receivers []*bitwire.Receiver
	sends     int
	closes    int
}

func (o *throughOrigin) Send([]string, bitwire.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sends++
	return nil
}
func (o *throughOrigin) Receive(receiver bitwire.Receiver) (func(), error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	attached := &receiver
	o.receivers = append(o.receivers, attached)
	return func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		for i, current := range o.receivers {
			if current == attached {
				o.receivers = append(o.receivers[:i], o.receivers[i+1:]...)
				return
			}
		}
	}, nil
}
func (o *throughOrigin) Close(bitwire.Code, string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closes++
	return nil
}
func (o *throughOrigin) attached() []*bitwire.Receiver {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]*bitwire.Receiver{}, o.receivers...)
}

func TestThroughSendsThroughAccessAndReceivesFromItsOrigin(t *testing.T) {
	origin, access := &throughOrigin{}, &declaredOrigin{}
	endpoint := duplex.Through(origin, access)
	if err := endpoint.Send([]string{"x"}, declaredEvent()); err != nil {
		t.Fatal(err)
	}
	if origin.sends != 0 || !reflect.DeepEqual(access.paths, [][]string{{"x"}}) {
		t.Fatal("a send bypassed the supplied access")
	}
	var delivered [][]string
	detach, err := endpoint.Receive(bitwire.Receiver{Message: func(path []string, _ bitwire.Message) { delivered = append(delivered, path) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := endpoint.Receive(bitwire.Receiver{}); !errors.Is(err, duplex.ErrReceiverExists) {
		t.Fatalf("second attachment: %v", err)
	}
	attached := origin.attached()
	if len(attached) != 1 {
		t.Fatalf("origin attachments = %d, want 1", len(attached))
	}
	attached[0].Message([]string{"y"}, declaredEvent())
	if !reflect.DeepEqual(delivered, [][]string{{"y"}}) {
		t.Fatal("a delivery did not come from the origin")
	}
	detach()
	detach()
	if len(origin.attached()) != 0 {
		t.Fatal("detach left the origin attached")
	}
	if _, err := endpoint.Receive(bitwire.Receiver{}); err != nil {
		t.Fatalf("attachment after detach: %v", err)
	}
}

func TestThroughClosureReleasesOnlyItsOwnAttachment(t *testing.T) {
	origin, access := &throughOrigin{}, &declaredOrigin{}
	endpoint := duplex.Through(origin, access)
	var closed []string
	if _, err := endpoint.Receive(bitwire.Receiver{Closed: func(_ bitwire.Code, reason string) { closed = append(closed, reason) }}); err != nil {
		t.Fatal(err)
	}
	if err := endpoint.Close(duplex.CodeNormal, "done"); err != nil {
		t.Fatal(err)
	}
	if err := endpoint.Close(duplex.CodeNormal, "again"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(closed, []string{"done"}) || len(origin.attached()) != 0 || origin.closes != 0 {
		t.Fatal("closure did not release exactly its own attachment, once, borrowing the origin")
	}
	if err := endpoint.Send(nil, declaredEvent()); !errors.Is(err, duplex.ErrClosed) {
		t.Fatalf("send after closure: %v", err)
	}
	if _, err := endpoint.Receive(bitwire.Receiver{}); !errors.Is(err, duplex.ErrClosed) {
		t.Fatalf("attachment after closure: %v", err)
	}
	if err := access.Send(nil, declaredEvent()); err != nil {
		t.Fatal("the access became unusable")
	}
	if _, err := origin.Receive(bitwire.Receiver{}); err != nil {
		t.Fatal("the origin became unusable")
	}
}

func TestThroughEndsWhenItsOriginEnds(t *testing.T) {
	origin, access := &throughOrigin{}, &declaredOrigin{}
	endpoint := duplex.Through(origin, access)
	var closed []bitwire.Code
	if _, err := endpoint.Receive(bitwire.Receiver{Closed: func(code bitwire.Code, _ string) { closed = append(closed, code) }}); err != nil {
		t.Fatal(err)
	}
	origin.attached()[0].Closed(duplex.CodeGoingAway, "gone")
	if !reflect.DeepEqual(closed, []bitwire.Code{duplex.CodeGoingAway}) {
		t.Fatal("the origin's ending was not delivered")
	}
	if err := endpoint.Send(nil, declaredEvent()); !errors.Is(err, duplex.ErrClosed) {
		t.Fatalf("send after the origin ended: %v", err)
	}
	if err := endpoint.Close(duplex.CodeNormal, "late"); err != nil || len(closed) != 1 {
		t.Fatal("a late closure was delivered again")
	}
}
