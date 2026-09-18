package tunnel

import (
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// The events below are what a tunnel tells about the channels over it. A
// tunnel observes through the peer it runs over and takes no observer of its
// own: what the outer connection does and what the channels over it do reach
// one observer, in the order they happened. A channel's own peer — one made
// over a Channel as a duplex.Conn — takes its own observer through its
// options, as a peer over any transport does.
//
// Never a payload: what a channel carries is the business of the peers that
// speak over it, and no byte of a frame reaches an event here.

// ChannelOpened is a channel coming into being, on the side that opened it
// and on the side that was asked: Opener says which side this is. The opener
// is told once the other side has accepted the open and declared its window;
// the other side once the open is admitted, or once the channel is taken,
// whichever comes first — so a channel is never taken on an observer before
// it opened on it.
type ChannelOpened struct {
	At     time.Time
	Family string
	ID     int64
	After  int64
	Opener bool
}

// ChannelAccepted is a channel the other side opened being taken here, by
// Accept or by Channel. It says no opener: the side that takes a channel is
// never the side that opened it.
type ChannelAccepted struct {
	At     time.Time
	Family string
	ID     int64
	After  int64
}

// ChannelClosed is a channel ending, once and whatever ended it: the close
// this side sent, the close the other side sent, the frame this side refused,
// or the going away of the connection carrying the channel. Code and Reason
// are what ended it, as the other side sees them.
type ChannelClosed struct {
	At     time.Time
	Family string
	ID     int64
	Code   int
	Reason string
}

// CreditStall is a send that found the other side's window full and waited
// for credit. Waiting is how many senders are then waiting on the channel,
// this one among them; a send that waits says so once, however long it waits
// and however often it wakes.
type CreditStall struct {
	At      time.Time
	Family  string
	ID      int64
	Waiting int
}

// OpenRefused is an open that opened nothing, on the side that refused it and
// on the side whose open was refused. Reason is what the refusal said, and it
// names no channel because none came into being.
type OpenRefused struct {
	At     time.Time
	Family string
	Reason string
}

func (ChannelOpened) ObserverEvent()   {}
func (ChannelAccepted) ObserverEvent() {}
func (ChannelClosed) ObserverEvent()   {}
func (CreditStall) ObserverEvent()     {}
func (OpenRefused) ObserverEvent()     {}

// The events of this layer are events of the runtime's observer, which is the
// only observer there is.
var (
	_ runtime.ObserverEvent = ChannelOpened{}
	_ runtime.ObserverEvent = ChannelAccepted{}
	_ runtime.ObserverEvent = ChannelClosed{}
	_ runtime.ObserverEvent = CreditStall{}
	_ runtime.ObserverEvent = OpenRefused{}
)

// Every hook point below asks the peer for an observer before it builds
// anything: a tunnel over a peer given none reads no clock and allocates no
// event for nobody, as the runtime's own hook points do.

// observeOpened tells of a channel that came into being, once and by whichever
// of the open and the take gets there first, and marks it as a channel whose
// close is the close of something.
func (t *Tunnel) observeOpened(c *Channel, opener bool) {
	c.announce.Do(func() {
		c.opened = true
		if t.peer.Observer() == nil {
			return
		}
		t.peer.Observe(ChannelOpened{At: time.Now(), Family: c.Family, ID: c.ID, After: c.After, Opener: opener})
	})
}

func (t *Tunnel) observeAccepted(c *Channel) {
	if t.peer.Observer() == nil {
		return
	}
	t.peer.Observe(ChannelAccepted{At: time.Now(), Family: c.Family, ID: c.ID, After: c.After})
}

func (t *Tunnel) observeClosed(c *Channel, code duplex.Code, reason string) {
	if t.peer.Observer() == nil {
		return
	}
	t.peer.Observe(ChannelClosed{At: time.Now(), Family: c.Family, ID: c.ID, Code: int(code), Reason: reason})
}

func (t *Tunnel) observeStall(c *Channel, waiting int) {
	if t.peer.Observer() == nil {
		return
	}
	t.peer.Observe(CreditStall{At: time.Now(), Family: c.Family, ID: c.ID, Waiting: waiting})
}

func (t *Tunnel) observeRefused(family, reason string) {
	if t.peer.Observer() == nil {
		return
	}
	t.peer.Observe(OpenRefused{At: time.Now(), Family: family, Reason: reason})
}

// refused tells of an open this side gives up on and returns what it gives up
// with, so that every path out of Open says why it opened nothing.
func (t *Tunnel) refused(family string, err error) error {
	t.observeRefused(family, err.Error())
	return err
}
