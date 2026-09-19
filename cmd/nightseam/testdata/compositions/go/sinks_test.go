package composition_test

// The sinks an application supplies, and the two further compositions — a
// cell and a topic — built out of the same two pieces: a reference handed in,
// and an implementation the holder calls back through. Each states its own
// behavior, because none of it is implied by the shape of the exchange.

import (
	"context"
	"fmt"
	"sync"
	"time"

	cellbinding "example.test/generated/api/go/cell-binding"
	cellprotocol "example.test/generated/api/go/cell-protocol"
	sinkbinding "example.test/generated/api/go/sink-binding"
	sinkclient "example.test/generated/api/go/sink-client"
	sinkprotocol "example.test/generated/api/go/sink-protocol"
	topicbinding "example.test/generated/api/go/topic-binding"
	topicprotocol "example.test/generated/api/go/topic-protocol"
	runtime "github.com/Bitspark/nightseam/runtime/go"
)

var _ sinkclient.Handler = (*recorder)(nil)

func (r *recorder) Report(ctx context.Context, _ *sinkclient.Client, params sinkprotocol.ReportRequest) (int64, error) {
	r.mu.Lock()
	blocked, gate, delay := r.blockFor, r.gate, r.delay
	r.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	if blocked != nil {
		if gate != nil {
			select {
			case gate <- struct{}{}:
			default:
			}
		}
		select {
		case <-blocked:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.taken++
	r.items = append(r.items, params.Sequence)
	if text := params.Item.Text; text != nil {
		r.texts = append(r.texts, text.Value)
	}
	if update := params.Item.Update; update != nil {
		r.texts = append(r.texts, fmt.Sprintf("%s@%d", update.At, update.Version))
	}
	return r.taken, nil
}

func (r *recorder) End(_ context.Context, _ *sinkclient.Client, params sinkprotocol.Ending) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endings++
	switch {
	case params.Done != nil:
		r.ended = "done"
	case params.Failed != nil:
		r.ended = "failed"
	default:
		r.ended = "cancelled"
	}
	return r.taken, nil
}

func (r *recorder) said() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.texts...)
}

// theCell is a cell composed from the same two pieces. Its stated behavior:
//
//   - consistency: one writer at a time, because set takes the lock that
//     reads the value and hands out the version; a watcher is told values in
//     version order and never a version it has already been told;
//   - completion: a watch ends when it is released or its connection goes;
//     the cell does not end the observer's sink, because the observer's
//     lifetime is the watcher's, not the cell's;
//   - backpressure: the cell awaits each report, so a slow watcher slows the
//     writer that provoked its update rather than growing a queue nobody
//     bounded. A cell that must not do that would drop or coalesce, and would
//     have to say which.
type theCell struct {
	mu       sync.Mutex
	scopes   map[*runtime.Peer]*scope
	value    cellprotocol.Value
	watchers map[string]*watcher
	next     int64
}

type watcher struct {
	token     string
	reference int64
	to        *sinkbinding.Remote
	scope     *scope
}

var _ cellbinding.Handler = (*theCell)(nil)

func newCell() *theCell {
	return &theCell{scopes: map[*runtime.Peer]*scope{}, watchers: map[string]*watcher{}}
}

func (c *theCell) attach(peer *runtime.Peer, s *scope) {
	c.mu.Lock()
	c.scopes[peer] = s
	c.mu.Unlock()
	go func() {
		<-peer.Done()
		s.close(context.Background())
		c.mu.Lock()
		for token, w := range c.watchers {
			if w.scope == s {
				delete(c.watchers, token)
			}
		}
		delete(c.scopes, peer)
		c.mu.Unlock()
	}()
}

func (c *theCell) scopeOf(peer *runtime.Peer) (*scope, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.scopes[peer]
	if !ok {
		return nil, errScopeClosed
	}
	return s, nil
}

func (c *theCell) Get(_ context.Context, _ *cellbinding.Remote) (cellprotocol.Value, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value, nil
}

func (c *theCell) Set(ctx context.Context, _ *cellbinding.Remote, params cellprotocol.SetRequest) (cellprotocol.Value, error) {
	c.mu.Lock()
	c.value = cellprotocol.Value{Text: params.Text, Version: c.value.Version + 1}
	written := c.value
	told := make([]*watcher, 0, len(c.watchers))
	for _, w := range c.watchers {
		told = append(told, w)
	}
	c.mu.Unlock()
	// Told in version order, and awaited: a watcher that does not answer
	// holds the write that provoked it. That is the cell's choice, stated.
	for _, w := range told {
		if _, err := w.to.Report(ctx, sinkprotocol.ReportRequest{
			Sequence: written.Version,
			Item:     sinkprotocol.Item{Update: &sinkprotocol.Update{At: written.Text, Version: written.Version}},
		}); err != nil {
			c.Unwatch(ctx, nil, cellprotocol.UnwatchRequest{Token: w.token})
		}
	}
	return written, nil
}

func (c *theCell) Watch(ctx context.Context, remote *cellbinding.Remote, params cellprotocol.Watch) (cellprotocol.Watching, error) {
	here, err := c.scopeOf(remote.Peer)
	if err != nil {
		return cellprotocol.Watching{}, err
	}
	to, err := here.importSink(ctx, params.Observer.Channel)
	if err != nil {
		return cellprotocol.Watching{}, &runtime.PublicError{Code: cellprotocol.ErrorUnknownReference, Message: err.Error()}
	}
	c.mu.Lock()
	c.next++
	token := fmt.Sprintf("w%d", c.next)
	c.watchers[token] = &watcher{token: token, reference: params.Observer.Channel, to: to, scope: here}
	at := c.value
	c.mu.Unlock()
	return cellprotocol.Watching{At: at, Token: token}, nil
}

func (c *theCell) Unwatch(_ context.Context, _ *cellbinding.Remote, params cellprotocol.UnwatchRequest) (bool, error) {
	c.mu.Lock()
	w, ok := c.watchers[params.Token]
	delete(c.watchers, params.Token)
	c.mu.Unlock()
	if !ok {
		return false, nil
	}
	// Releasing the watch releases the reference the watch took, and calls
	// nothing of the observer: the observer's own lifetime is not the cell's.
	w.scope.release(w.reference)
	return true, nil
}

// theTopic is a topic. Its stated behavior:
//
//   - ordering: publish takes a sequence under the lock, and every subscriber
//     is told messages in that sequence order, because one goroutine per
//     subscriber sends them in order and awaits each;
//   - completion: a subscription ends on unsubscribe or with its connection;
//     the topic never ends a subscriber's sink, since a subscriber may be
//     watching several topics through one;
//   - backpressure: a subscriber is served from a bounded queue of its own,
//     and a subscriber that fills it is dropped rather than allowed to stall
//     the publisher. That is the opposite choice from the cell's, made
//     deliberately and visible in the test that holds it.
type theTopic struct {
	mu     sync.Mutex
	scopes map[*runtime.Peer]*scope
	subs   map[string]*subscriber
	next   int64
	seq    int64
}

type subscriber struct {
	token     string
	topic     string
	reference int64
	to        *sinkbinding.Remote
	scope     *scope
	queue     chan sinkprotocol.ReportRequest
	dropped   bool
	done      chan struct{}
}

var _ topicbinding.Handler = (*theTopic)(nil)

func newTopic() *theTopic {
	return &theTopic{scopes: map[*runtime.Peer]*scope{}, subs: map[string]*subscriber{}}
}

func (t *theTopic) attach(peer *runtime.Peer, s *scope) {
	t.mu.Lock()
	t.scopes[peer] = s
	t.mu.Unlock()
	go func() {
		<-peer.Done()
		s.close(context.Background())
		t.mu.Lock()
		for token, sub := range t.subs {
			if sub.scope == s {
				delete(t.subs, token)
				close(sub.queue)
			}
		}
		delete(t.scopes, peer)
		t.mu.Unlock()
	}()
}

func (t *theTopic) scopeOf(peer *runtime.Peer) (*scope, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.scopes[peer]
	if !ok {
		return nil, errScopeClosed
	}
	return s, nil
}

func (t *theTopic) Publish(_ context.Context, _ *topicbinding.Remote, params topicprotocol.Message) (int64, error) {
	t.mu.Lock()
	t.seq++
	sequence := t.seq
	told := make([]*subscriber, 0, len(t.subs))
	for _, sub := range t.subs {
		if sub.topic == params.Topic {
			told = append(told, sub)
		}
	}
	t.mu.Unlock()
	body := sinkprotocol.ItemTextValue{Value: params.Body}
	for _, sub := range told {
		report := sinkprotocol.ReportRequest{Sequence: sequence, Item: sinkprotocol.Item{Text: &body}}
		select {
		case sub.queue <- report:
		default:
			// The queue is full: this subscriber is dropped rather than
			// allowed to hold the publisher. The choice is the topic's.
			t.mu.Lock()
			sub.dropped = true
			t.mu.Unlock()
		}
	}
	return sequence, nil
}

func (t *theTopic) Subscribe(ctx context.Context, remote *topicbinding.Remote, params topicprotocol.Subscribe) (topicprotocol.Subscription, error) {
	here, err := t.scopeOf(remote.Peer)
	if err != nil {
		return topicprotocol.Subscription{}, err
	}
	to, err := here.importSink(ctx, params.Subscriber.Channel)
	if err != nil {
		return topicprotocol.Subscription{}, &runtime.PublicError{Code: topicprotocol.ErrorUnknownReference, Message: err.Error()}
	}
	t.mu.Lock()
	t.next++
	sub := &subscriber{
		token:     fmt.Sprintf("s%d", t.next),
		topic:     params.Topic,
		reference: params.Subscriber.Channel,
		to:        to,
		scope:     here,
		queue:     make(chan sinkprotocol.ReportRequest, 8),
		done:      make(chan struct{}),
	}
	t.subs[sub.token] = sub
	from := t.seq
	t.mu.Unlock()
	go sub.deliver(here.context())
	return topicprotocol.Subscription{Token: sub.token, From: from}, nil
}

func (t *theTopic) Unsubscribe(_ context.Context, _ *topicbinding.Remote, params topicprotocol.UnsubscribeRequest) (bool, error) {
	t.mu.Lock()
	sub, ok := t.subs[params.Token]
	delete(t.subs, params.Token)
	t.mu.Unlock()
	if !ok {
		return false, nil
	}
	close(sub.queue)
	<-sub.done
	sub.scope.release(sub.reference)
	return true, nil
}

func (t *theTopic) droppedAny() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, sub := range t.subs {
		if sub.dropped {
			return true
		}
	}
	return false
}

// deliver is the one goroutine that sends to a subscriber, which is what
// makes the order it is told the order the topic gave.
func (s *subscriber) deliver(ctx context.Context) {
	defer close(s.done)
	for report := range s.queue {
		if _, err := s.to.Report(ctx, report); err != nil {
			return
		}
	}
}
