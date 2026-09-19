package composition_test

// Forwarding, and the compositions a live layer is usually asked for — an
// interface, a stream, a cell and a topic. Each is a declaration and some
// application code over the same two pieces, and each states the behavior it
// actually has rather than inheriting one from the exchange's shape.

import (
	"testing"

	cellprotocol "example.test/generated/api/go/cell-protocol"
	topicprotocol "example.test/generated/api/go/topic-protocol"
	workerprotocol "example.test/generated/api/go/worker-protocol"
)

// 10. Explicit forwarding. A worker imports a reference on one connection and
// re-exports a relay of it into another, so a second worker can report
// through it. The two bindings have their own lifetimes: the forwarded one is
// the second connection's, the origin one is the first's, and only the origin
// going away reaches the relay — as a failed call, not as silence.
func TestForwardingAnImportedReference(t *testing.T) {
	ctx := testContext(t)
	near, far := serveWorkers(t), serveWorkers(t)
	link(t, ctx, near, far)
	called := dialWorkers(t, ctx, near)

	sink := newRecorder()
	reference, err := called.scope.exportSink(ctx, sink)
	if err != nil {
		t.Fatal(err)
	}
	counted, err := called.client.Forward(ctx, workerprotocol.Forward{
		Progress: workerprotocol.Handle{Channel: reference},
		Ticket:   workerprotocol.Ticket{Label: "forwarded", Steps: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if counted.Resolved != 1 {
		t.Fatalf("the near worker forwarded %d references", counted.Resolved)
	}
	// The far worker's reports arrive at the caller's own sink, through the
	// relay the near worker exported into the second connection.
	eventually(t, "the forwarded reports reach the origin sink", func() bool {
		_, _, ended, endings := sink.snapshot()
		return ended == "done" && endings == 1
	})
	if taken, _, _, _ := sink.snapshot(); taken != 3 {
		t.Fatalf("the origin sink took %d of the three forwarded reports", taken)
	}
	forwarded := near.worker.forwarded
	relayed, failed := forwarded.counts()
	if relayed != 3 || failed != nil {
		t.Fatalf("the relay passed %d reports and failed with %v", relayed, failed)
	}

	// Releasing the forwarding binding does not release the origin one. The
	// near worker revokes what it exported into the second connection, and
	// still holds — and can still use — the reference the caller gave it.
	if !near.worker.originScope.revoke(ctx, forwarded.self) {
		t.Fatal("the forwarding binding was not there to revoke")
	}
	if _, err := called.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "after", Steps: 2}, Progress: workerprotocol.Handle{Channel: reference},
	}); err != nil {
		t.Fatalf("the origin reference died with the forwarding binding: %v", err)
	}
	eventually(t, "the origin sink is still reachable after the forwarding binding went", func() bool {
		taken, _, _, _ := sink.snapshot()
		return taken == 5
	})
}

// The other half of the forwarding contract: the origin going away reaches
// the relay as a failure, not as silence. The caller's sink is held shut so
// that a relayed report is in flight when its connection dies.
func TestForwardingFailsWhenTheOriginGoes(t *testing.T) {
	ctx := testContext(t)
	near, far := serveWorkers(t), serveWorkers(t)
	link(t, ctx, near, far)
	called := dialWorkers(t, ctx, near)

	sink := newRecorder()
	sink.hold()
	reference, err := called.scope.exportSink(ctx, sink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := called.client.Forward(ctx, workerprotocol.Forward{
		Progress: workerprotocol.Handle{Channel: reference},
		Ticket:   workerprotocol.Ticket{Label: "orphan", Steps: 4},
	}); err != nil {
		t.Fatal(err)
	}
	sink.waitForReport(t)
	_ = called.client.Close()
	eventually(t, "the relay learns the origin went, as a failed call", func() bool {
		_, failed := near.worker.forwarded.counts()
		return failed != nil
	})
	// And the far worker's job settles rather than reporting into nothing.
	eventually(t, "the far worker's job settles", func() bool {
		job := far.worker.jobFor("orphan")
		return job != nil && job.Ending() != ""
	})
}

// 11. The derived compositions. An interface is a product of callables; a
// stream is an interface plus an ordering and a completion the application
// states; a cell and a topic are the same two pieces with different, stated
// answers about consistency and backpressure. None of that is in the wire.
func TestDerivedInterfaceAndStream(t *testing.T) {
	ctx := testContext(t)
	workers := serveWorkers(t)
	called := dialWorkers(t, ctx, workers)

	// The stream. Its ordering is the worker awaiting each report before the
	// next; its completion is exactly one End; its cancellation is the job's
	// own operation; its backpressure is the sink's answer, which is what the
	// worker waits on. Holding the sink shut therefore holds the worker.
	sink := newRecorder()
	sink.hold()
	reference, err := called.scope.exportSink(ctx, sink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := called.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "stream", Steps: 5}, Progress: workerprotocol.Handle{Channel: reference},
	}); err != nil {
		t.Fatal(err)
	}
	sink.waitForReport(t)
	if taken, _, _, _ := sink.snapshot(); taken != 0 {
		t.Fatalf("a held sink took %d items; the worker is not paced by it", taken)
	}
	sink.resume()
	eventually(t, "the stream completes once", func() bool {
		_, _, ended, endings := sink.snapshot()
		return ended == "done" && endings == 1
	})
	_, items, _, _ := sink.snapshot()
	for i, sequence := range items {
		if sequence != int64(i+1) {
			t.Fatalf("the stream arrived as %v, not in order", items)
		}
	}
}

// The cell: one writer at a time, watchers told in version order, and a slow
// watcher holding the write that provoked it. The last of those is the cell's
// own choice about backpressure, and the opposite of the topic's below.
func TestDerivedCell(t *testing.T) {
	ctx := testContext(t)
	cells := serveCells(t)
	called := dialCells(t, ctx, cells)

	observer := newRecorder()
	reference, err := called.scope.exportSink(ctx, observer)
	if err != nil {
		t.Fatal(err)
	}
	watching, err := called.client.Watch(ctx, cellprotocol.Watch{Observer: cellprotocol.Handle{Channel: reference}})
	if err != nil {
		t.Fatal(err)
	}
	if watching.At.Version != 0 {
		t.Fatalf("a fresh cell watched from version %d", watching.At.Version)
	}
	for _, text := range []string{"one", "two", "three"} {
		written, err := called.client.Set(ctx, cellprotocol.SetRequest{Text: text})
		if err != nil {
			t.Fatal(err)
		}
		if written.Text != text {
			t.Fatalf("the cell holds %q after writing %q", written.Text, text)
		}
	}
	eventually(t, "the watcher was told all three versions", func() bool {
		return len(observer.said()) == 3
	})
	said := observer.said()
	for i, want := range []string{"one@1", "two@2", "three@3"} {
		if said[i] != want {
			t.Fatalf("the watcher was told %v, not in version order", said)
		}
	}
	if value, err := called.client.Get(ctx); err != nil || value.Version != 3 || value.Text != "three" {
		t.Fatalf("the cell reads %#v, %v", value, err)
	}

	// Unwatching releases the reference the watch took and calls nothing of
	// the observer: the observer's lifetime is the watcher's, not the cell's.
	if released, err := called.client.Unwatch(ctx, cellprotocol.UnwatchRequest{Token: watching.Token}); err != nil || !released {
		t.Fatalf("unwatch answered %v, %v", released, err)
	}
	if _, err := called.client.Set(ctx, cellprotocol.SetRequest{Text: "four"}); err != nil {
		t.Fatal(err)
	}
	if _, _, ended, endings := observer.snapshot(); ended != "" || endings != 0 {
		t.Fatalf("unwatching ended the observer %q after %d endings", ended, endings)
	}
	if got := len(observer.said()); got != 3 {
		t.Fatalf("a released watcher was told %d updates", got)
	}
}

// The topic: fan-out, one sequence for everybody, and a subscriber that
// cannot keep up dropped rather than allowed to hold the publisher. That is
// the choice the cell does not make, and the test holds both.
func TestDerivedTopic(t *testing.T) {
	ctx := testContext(t)
	topics := serveTopics(t)
	first, second := dialTopics(t, ctx, topics), dialTopics(t, ctx, topics)

	one, two := newRecorder(), newRecorder()
	referenceOne, err := first.scope.exportSink(ctx, one)
	if err != nil {
		t.Fatal(err)
	}
	referenceTwo, err := second.scope.exportSink(ctx, two)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionOne, err := first.client.Subscribe(ctx, topicprotocol.Subscribe{
		Topic: "weather", Subscriber: topicprotocol.Handle{Channel: referenceOne},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.client.Subscribe(ctx, topicprotocol.Subscribe{
		Topic: "weather", Subscriber: topicprotocol.Handle{Channel: referenceTwo},
	}); err != nil {
		t.Fatal(err)
	}
	// A message on another topic reaches neither.
	if _, err := first.client.Publish(ctx, topicprotocol.Message{Topic: "traffic", Body: "elsewhere"}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"rain", "sun"} {
		if _, err := first.client.Publish(ctx, topicprotocol.Message{Topic: "weather", Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "both subscribers were told both messages", func() bool {
		return len(one.said()) == 2 && len(two.said()) == 2
	})
	for _, said := range [][]string{one.said(), two.said()} {
		if said[0] != "rain" || said[1] != "sun" {
			t.Fatalf("a subscriber was told %v, not in the published order", said)
		}
	}
	_, items, _, _ := one.snapshot()
	if items[0] >= items[1] {
		t.Fatalf("the topic's sequences arrived as %v", items)
	}

	// Unsubscribing releases that subscriber's reference and stops its
	// delivery, and leaves the other alone.
	if gone, err := first.client.Unsubscribe(ctx, topicprotocol.UnsubscribeRequest{Token: subscriptionOne.Token}); err != nil || !gone {
		t.Fatalf("unsubscribe answered %v, %v", gone, err)
	}
	if _, err := first.client.Publish(ctx, topicprotocol.Message{Topic: "weather", Body: "fog"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the remaining subscriber was told the third message", func() bool {
		return len(two.said()) == 3
	})
	if got := len(one.said()); got != 2 {
		t.Fatalf("an unsubscribed subscriber was told %d messages", got)
	}
	if _, _, ended, _ := one.snapshot(); ended != "" {
		t.Fatalf("unsubscribing ended the subscriber's sink as %q", ended)
	}
}

// The topic's backpressure, which is the opposite of the cell's: a subscriber
// that does not answer fills its own bounded queue and is dropped, and the
// publisher is never held by it.
func TestATopicDropsRatherThanStalls(t *testing.T) {
	ctx := testContext(t)
	topics := serveTopics(t)
	called := dialTopics(t, ctx, topics)

	slow := newRecorder()
	slow.hold()
	reference, err := called.scope.exportSink(ctx, slow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := called.client.Subscribe(ctx, topicprotocol.Subscribe{
		Topic: "flood", Subscriber: topicprotocol.Handle{Channel: reference},
	}); err != nil {
		t.Fatal(err)
	}
	// Far more than the subscriber's queue holds. Every publish answers, and
	// answers promptly, which is the promise: the publisher is not the
	// subscriber's hostage.
	for i := range 64 {
		sequence, err := called.client.Publish(ctx, topicprotocol.Message{Topic: "flood", Body: "x"})
		if err != nil {
			t.Fatalf("publish %d was refused: %v", i, err)
		}
		if sequence != int64(i+1) {
			t.Fatalf("publish %d took sequence %d", i, sequence)
		}
	}
	eventually(t, "the subscriber that could not keep up was dropped", topics.topic.droppedAny)
	slow.resume()
}
