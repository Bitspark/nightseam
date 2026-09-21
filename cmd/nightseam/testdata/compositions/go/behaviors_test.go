package composition_test

// The eight behaviors v0.5.0 asks a live layer for, each run against the
// composition and each stating what it actually holds. Where the basis does
// not supply a rule, the test says so in as many words and
// docs/runtime/compositions.md carries it to #201 and #202.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	jobbinding "example.test/generated/api/go/job-binding"
	jobprotocol "example.test/generated/api/go/job-protocol"
	notesprotocol "example.test/generated/api/go/notes-protocol"
	workerprotocol "example.test/generated/api/go/worker-protocol"
	runtime "github.com/Bitspark/nightseam/runtime/go"
)

// 1. Data alone. Nothing in this case makes a peer, a connection, a tunnel or
// a scope: a model-tier family encodes, decodes and validates on its own, and
// the generated package it needs has no live anything in it. That is the
// independence check #200's table asks for, run rather than asserted.
func TestDataAloneNeedsNoTunnel(t *testing.T) {
	notebook := notesprotocol.Notebook{
		Title: "field",
		Notes: []notesprotocol.Note{{ID: "first", Body: "one", Tags: []string{"a"}, Revision: 1}},
		ByID:  map[string]notesprotocol.Note{"first": {ID: "first", Body: "one", Tags: []string{"a"}, Revision: 1}},
		Latest: runtime.Some(runtime.NonNull(notesprotocol.Note{
			ID: "first", Body: "one", Tags: []string{"a"}, Revision: 1,
		})),
	}
	encoded, err := notebook.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var read notesprotocol.Notebook
	if err := read.UnmarshalJSON(encoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(notebook, read) {
		t.Fatalf("the notebook did not survive its own encoding:\n%#v\n%#v", notebook, read)
	}
	// The constraints of the declaration are the validator's, with no
	// connection to check them over.
	if err := notesprotocol.ValidateValue("Note", map[string]any{
		"id": "Not Lower", "body": "x", "tags": []string{}, "revision": 0,
	}); err == nil {
		t.Fatal("a value breaking the declared pattern validated")
	}
	if err := notesprotocol.ValidateValue("Note", map[string]any{
		"id": "ok", "body": "x", "tags": []string{}, "revision": -1,
	}); err == nil {
		t.Fatal("a value breaking the declared minimum validated")
	}
}

// 2. A supplied callback and a returned callable, in one exchange, with the
// job's own contract held: reports in order, one ending, and a status a
// caller can read off the returned reference.
func TestSuppliedCallbackAndReturnedCallable(t *testing.T) {
	ctx := testContext(t)
	workers := serveWorkers(t)
	called := dialWorkers(t, ctx, workers)

	sink := newRecorder()
	reference, err := called.scope.exportSink(ctx, sink)
	if err != nil {
		t.Fatal(err)
	}
	started, err := called.client.Start(ctx, workerprotocol.Start{
		Ticket:   workerprotocol.Ticket{Label: "one", Steps: 4},
		Progress: workerprotocol.Handle{Channel: reference},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !started.Accepted {
		t.Fatal("the worker did not accept the ticket")
	}
	// The returned reference is a callable interface, not a stream: it has
	// two operations and says nothing about how progress arrives.
	job, err := called.scope.importJob(ctx, started.Job.Channel)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "four reports and one ending", func() bool {
		_, _, ended, endings := sink.snapshot()
		return ended == "done" && endings == 1
	})
	_, items, _, endings := sink.snapshot()
	if !reflect.DeepEqual(items, []int64{1, 2, 3, 4}) {
		t.Fatalf("the sink was told %v, not 1 2 3 4 in order", items)
	}
	if endings != 1 {
		t.Fatalf("the sink was ended %d times", endings)
	}
	status, err := job.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "done" || status.Delivered != 4 {
		t.Fatalf("the job says %#v", status)
	}
	// The callback and status become visible before the worker receives the
	// final acknowledgement. Only its completion channel says it has ended.
	finished := workers.worker.jobFor("one")
	if finished == nil {
		t.Fatal("the worker made no job")
	}
	select {
	case <-finished.done:
	case <-ctx.Done():
		t.Fatalf("waiting for worker completion: %v", ctx.Err())
	}
	// Cancelling a job that has ended is the application's refusal, by the
	// code the family declares.
	if _, err := job.Cancel(ctx); !jobprotocol.IsError(err, jobprotocol.ErrorJobFinished) {
		t.Fatalf("cancelling a finished job answered %v", err)
	}
}

// 3. Two clients, two references, no crossing. Their channel ids are equal,
// which is the whole point: a reference is an id *on a connection*, and the
// worker keeps one table per connection because nothing in the id says which
// connection it came from.
func TestTwoClientsKeepTheirOwnReferences(t *testing.T) {
	ctx := testContext(t)
	workers := serveWorkers(t)
	first, second := dialWorkers(t, ctx, workers), dialWorkers(t, ctx, workers)

	one, two := newRecorder(), newRecorder()
	referenceOne, err := first.scope.exportSink(ctx, one)
	if err != nil {
		t.Fatal(err)
	}
	referenceTwo, err := second.scope.exportSink(ctx, two)
	if err != nil {
		t.Fatal(err)
	}
	if referenceOne != referenceTwo {
		t.Fatalf("the two callers minted %d and %d; this case is only a proof while they are equal", referenceOne, referenceTwo)
	}
	if _, err := first.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "first", Steps: 2}, Progress: workerprotocol.Handle{Channel: referenceOne},
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the first caller's sink is done", func() bool {
		_, _, ended, _ := one.snapshot()
		return ended == "done"
	})
	if taken, _, ended, _ := two.snapshot(); taken != 0 || ended != "" {
		t.Fatalf("the second caller's sink was told %d items and ended %q by the first caller's job", taken, ended)
	}
	if _, err := second.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "second", Steps: 3}, Progress: workerprotocol.Handle{Channel: referenceTwo},
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the second caller's sink is done", func() bool {
		_, _, ended, _ := two.snapshot()
		return ended == "done"
	})
	if taken, _, _, _ := one.snapshot(); taken != 2 {
		t.Fatalf("the first caller's sink took %d items, not its own 2", taken)
	}
	if taken, _, _, _ := two.snapshot(); taken != 3 {
		t.Fatalf("the second caller's sink took %d items, not its own 3", taken)
	}
}

// 4. The returned callable invokes the supplied one after the call that
// created it returned. The sink is held shut before the exchange, so Start
// can be seen to have answered with nothing reported yet; then the sink is
// let go and the reports arrive. Cancel — an operation of the *returned*
// reference — then reaches the *supplied* one.
func TestAReturnedCallableCallsASuppliedOneLater(t *testing.T) {
	ctx := testContext(t)
	workers := serveWorkers(t)
	called := dialWorkers(t, ctx, workers)

	sink := newRecorder()
	sink.hold()
	sink.slowBy(5 * time.Millisecond)
	reference, err := called.scope.exportSink(ctx, sink)
	if err != nil {
		t.Fatal(err)
	}
	started, err := called.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "later", Steps: 8}, Progress: workerprotocol.Handle{Channel: reference},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Start has returned. The sink is holding the first report, which is a
	// call the worker made *after* answering.
	sink.waitForReport(t)
	if taken, _, ended, _ := sink.snapshot(); taken != 0 || ended != "" {
		t.Fatalf("the sink had taken %d items and ended %q before Start returned", taken, ended)
	}
	sink.resume()
	eventually(t, "the held sink is told everything once released", func() bool {
		taken, _, _, _ := sink.snapshot()
		return taken >= 1
	})

	job, err := called.scope.importJob(ctx, started.Job.Channel)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := job.Cancel(ctx)
	if err != nil && !jobprotocol.IsError(err, jobprotocol.ErrorJobFinished) {
		t.Fatal(err)
	}
	eventually(t, "the sink is ended exactly once", func() bool {
		_, _, ended, endings := sink.snapshot()
		return ended != "" && endings == 1
	})
	// The completion race, held rather than avoided: a cancellation that
	// arrives while the last report is in flight loses, and the answer says
	// so. What must never happen is the two disagreeing.
	_, _, ended, _ := sink.snapshot()
	if err == nil && cancelled.Stopped != (ended == "cancelled") {
		t.Fatalf("the job answered %#v and ended its sink %q", cancelled, ended)
	}
	if err != nil && ended == "cancelled" {
		t.Fatalf("the job refused the cancellation as finished and then ended its sink cancelled")
	}
}

// 5. Every position the language admits a reference in. One message carries a
// reference in a field, in an array, in a map, in a nullable, in a union arm
// and in an inline shape; the worker resolves each, and one made-up id in the
// array is refused rather than resolved to something else.
func TestReferencesInProductsSumsAndContainers(t *testing.T) {
	ctx := testContext(t)
	workers := serveWorkers(t)
	called := dialWorkers(t, ctx, workers)

	references := make([]workerprotocol.Handle, 0, 6)
	for range 6 {
		reference, err := called.scope.exportSink(ctx, newRecorder())
		if err != nil {
			t.Fatal(err)
		}
		references = append(references, workerprotocol.Handle{Channel: reference})
	}
	held := workerprotocol.Held{
		One:    references[0],
		Many:   []workerprotocol.Handle{references[1], {Channel: 9999}},
		ByName: map[string]workerprotocol.Handle{"a": references[2]},
		Maybe:  runtime.Some(runtime.NonNull(references[3])),
		Either: workerprotocol.Either{Sink: &references[4]},
		Inline: workerprotocol.HeldInline{Held: references[5], Label: "inline"},
	}
	counted, err := called.client.Collect(ctx, held)
	if err != nil {
		t.Fatal(err)
	}
	if counted.Resolved != 6 || counted.Refused != 1 {
		t.Fatalf("the worker resolved %d and refused %d of six references and one invention", counted.Resolved, counted.Refused)
	}
}

// 6. Aliasing and release. Importing the same reference twice is one
// attachment with two aliases; releasing once leaves it usable; releasing the
// last alias closes it, and calls nothing of the implementation behind it.
func TestRepeatedImportAndRelease(t *testing.T) {
	ctx := testContext(t)
	workers := serveWorkers(t)
	called := dialWorkers(t, ctx, workers)

	sink := newRecorder()
	reference, err := called.scope.exportSink(ctx, sink)
	if err != nil {
		t.Fatal(err)
	}
	started, err := called.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "aliased", Steps: 2}, Progress: workerprotocol.Handle{Channel: reference},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := called.scope.importJob(ctx, started.Job.Channel)
	if err != nil {
		t.Fatal(err)
	}
	second, err := called.scope.importJob(ctx, started.Job.Channel)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("two imports of one reference gave two attachments")
	}
	if got := called.scope.aliases(started.Job.Channel); got != 2 {
		t.Fatalf("the scope holds %d aliases, not 2", got)
	}
	if !called.scope.release(started.Job.Channel) {
		t.Fatal("releasing an alias reported nothing released")
	}
	if _, err := second.Status(ctx); err != nil {
		t.Fatalf("an alias died with its sibling: %v", err)
	}
	// Releasing the last alias closes the attachment. Nothing of the job is
	// called: the sink keeps whatever ending the job itself gives it.
	if !called.scope.release(started.Job.Channel) {
		t.Fatal("releasing the last alias reported nothing released")
	}
	if got := called.scope.aliases(started.Job.Channel); got != 0 {
		t.Fatalf("%d aliases survived the release", got)
	}
	if _, err := second.Status(ctx); err == nil {
		t.Fatal("a released reference still answered")
	}
	// Release is idempotent, and releasing what was never imported is not an
	// error either.
	if called.scope.release(started.Job.Channel) {
		t.Fatal("releasing a released reference reported a release")
	}
	eventually(t, "the job runs on and ends its sink, which release did not touch", func() bool {
		_, _, ended, endings := sink.snapshot()
		return ended == "done" && endings == 1
	})
}

// Repeated channel acquisition now shares one eagerly prepared Wire. Two
// successive scalar interpretations use that same correlation owner and both answer; the
// consumer table above separately owns aliasing and release.
func TestGeneratedModelsSharePreparedChannel(t *testing.T) {
	ctx := testContext(t)
	workers := serveWorkers(t)
	called := dialWorkers(t, ctx, workers)
	reference, err := called.scope.exportSink(ctx, newRecorder())
	if err != nil {
		t.Fatal(err)
	}
	started, err := called.client.Start(ctx, workerprotocol.Start{Ticket: workerprotocol.Ticket{Label: "shared", Steps: 1}, Progress: workerprotocol.Handle{Channel: reference}})
	if err != nil {
		t.Fatal(err)
	}
	one, ok, err := called.carrier.Channel(started.Job.Channel, runtime.Options{})
	if err != nil || !ok {
		t.Fatalf("channel: %v, %v", ok, err)
	}
	two, ok, err := called.carrier.Channel(started.Job.Channel, runtime.Options{})
	if err != nil || !ok {
		t.Fatalf("channel again: %v, %v", ok, err)
	}
	if one != two {
		t.Fatal("repeated acquisition made a second channel Wire")
	}
	if one.Family != "job" {
		t.Fatalf("the channel speaks %q", one.Family)
	}
	if one.Digest != jobprotocol.WireDigest() {
		t.Fatalf("the channel lost its declaration digest: %q", one.Digest)
	}
	for range 2 {
		complete, cleanup, err := jobbinding.PrepareFromWire(one, runtime.AdapterContext{})
		if err != nil {
			t.Fatal(err)
		}
		factory, err := complete(ctx)
		if err != nil {
			cleanup()
			t.Fatal(err)
		}
		model, err := factory(jobprotocol.Client{Methods: struct{}{}, Events: struct{}{}})
		if err != nil {
			cleanup()
			t.Fatal(err)
		}
		_, err = model.Methods.Status(ctx)
		cleanup()
		if err != nil {
			t.Fatal(err)
		}
	}
}

// 7. A wrong contract, a reference minted here, and a reference from another
// connection. The first two are refused; the third is the one the basis
// cannot refuse, and the test says exactly what happens instead.
func TestWrongContractOwnAndForeignReferences(t *testing.T) {
	ctx := testContext(t)
	workers := serveWorkers(t)
	first, second := dialWorkers(t, ctx, workers), dialWorkers(t, ctx, workers)

	one, two := newRecorder(), newRecorder()
	referenceOne, err := first.scope.exportSink(ctx, one)
	if err != nil {
		t.Fatal(err)
	}
	referenceTwo, err := second.scope.exportSink(ctx, two)
	if err != nil {
		t.Fatal(err)
	}
	started, err := first.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "contracts", Steps: 1}, Progress: workerprotocol.Handle{Channel: referenceOne},
	})
	if err != nil {
		t.Fatal(err)
	}

	// A wrong contract: the job's reference, imported as a sink.
	if _, err := first.scope.importSink(ctx, started.Job.Channel); !errors.Is(err, errWrongContract) {
		t.Fatalf("a job imported as a sink answered %v", err)
	}
	// A reference this side minted is not one this side imports.
	if _, err := first.scope.importJob(ctx, referenceOne); !errors.Is(err, errOwnReference) {
		t.Fatalf("importing an own binding answered %v", err)
	}
	// A reference that names nothing.
	if _, err := first.scope.importJob(ctx, 4242); !errors.Is(err, errUnknownReference) {
		t.Fatalf("importing an invented reference answered %v", err)
	}
	// A wrong contract on the wire: the worker is handed a job reference
	// where a sink belongs, and refuses by the code the family declares.
	if _, err := first.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "miscast", Steps: 1}, Progress: started.Job,
	}); !workerprotocol.IsError(err, workerprotocol.ErrorUnknownReference) {
		t.Fatalf("the worker took a job where a sink belongs: %v", err)
	}

	// The obstruction. `{"channel": N}` carries no evidence of the connection
	// it was minted on, so the second caller can send the first caller's
	// number and it is not refused — it resolves, in the second caller's own
	// scope, to the second caller's own binding. Nothing crosses, because the
	// table is per connection; but nothing *refuses* either, and an
	// application that kept one table for every connection would cross.
	if referenceOne != referenceTwo {
		t.Skip("the two callers' ids differ, so this case proves nothing here")
	}
	if _, err := second.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "replayed", Steps: 2}, Progress: workerprotocol.Handle{Channel: referenceOne},
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the replayed number reached the second caller's own sink", func() bool {
		_, _, ended, _ := two.snapshot()
		return ended == "done"
	})
	if taken, _, _, _ := two.snapshot(); taken != 2 {
		t.Fatalf("the second caller's sink took %d of its own 2 items", taken)
	}
	if taken, _, _, _ := one.snapshot(); taken != 1 {
		t.Fatalf("the first caller's sink took %d items; the replayed reference reached it", taken)
	}
}

// 8. Three cancellations, three outcomes. Cancelling a call ends that call
// and nothing else; releasing a reference closes an attachment and calls
// nothing; cancelling the application's job stops the work and ends the sink.
func TestReleaseCancelAndJobCancelDiffer(t *testing.T) {
	ctx := testContext(t)
	workers := serveWorkers(t)
	called := dialWorkers(t, ctx, workers)

	sink := newRecorder()
	// The job must still be running when all three cancellations reach it,
	// so the sink takes its time: five milliseconds a report over sixteen of
	// them is a job nothing in this case can outrun.
	sink.slowBy(5 * time.Millisecond)
	reference, err := called.scope.exportSink(ctx, sink)
	if err != nil {
		t.Fatal(err)
	}
	started, err := called.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "three", Steps: 16}, Progress: workerprotocol.Handle{Channel: reference},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := called.scope.importJob(ctx, started.Job.Channel)
	if err != nil {
		t.Fatal(err)
	}

	// (a) An RPC cancelled. The call ends cancelled; no reference is
	// released, and the job is untouched.
	slow, stop := context.WithCancel(ctx)
	failed := make(chan error, 1)
	go func() {
		_, err := called.client.Slow(slow, workerprotocol.Ticket{Label: "slow", Steps: 1})
		failed <- err
	}()
	eventually(t, "the slow call reached the worker", func() bool { return workers.worker.slows.Load() == 1 })
	stop()
	if err := <-failed; err == nil {
		t.Fatal("a cancelled call answered")
	}
	if got := called.scope.aliases(started.Job.Channel); got != 1 {
		t.Fatalf("cancelling a call changed the references held, to %d", got)
	}
	if status, err := job.Status(ctx); err != nil || status.State != "running" {
		t.Fatalf("cancelling a call reached the job: %#v, %v", status, err)
	}

	// (b) The job's own cancellation. The work stops, the sink is ended
	// cancelled, and the answer says what had been delivered.
	cancelled, err := job.Cancel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !cancelled.Stopped {
		t.Fatalf("the job answered %#v", cancelled)
	}
	eventually(t, "the sink is ended cancelled", func() bool {
		_, _, ended, endings := sink.snapshot()
		return ended == "cancelled" && endings == 1
	})
	taken, _, _, _ := sink.snapshot()
	if cancelled.Delivered != taken {
		t.Fatalf("the job says %d delivered and the sink took %d", cancelled.Delivered, taken)
	}

	// (c) A reference released. The attachment closes; nothing of the
	// implementation is called, so the sink's ending is still the job's one.
	if !called.scope.release(started.Job.Channel) {
		t.Fatal("releasing the job reported nothing released")
	}
	if _, err := job.Status(ctx); err == nil {
		t.Fatal("a released reference answered")
	}
	if _, _, ended, endings := sink.snapshot(); ended != "cancelled" || endings != 1 {
		t.Fatalf("releasing a reference changed the sink's ending to %q after %d endings", ended, endings)
	}
}

// 9. Closure, and what a new connection does not revive. Closing the caller's
// connection settles the work that was running on it: the job's next report
// fails and the job ends, rather than the worker holding a reference to
// nothing. A second connection gets a scope of its own, and the id that meant
// something on the first means nothing in it.
func TestCloseSettlesPendingWork(t *testing.T) {
	ctx := testContext(t)
	workers := serveWorkers(t)
	called := dialWorkers(t, ctx, workers)

	sink := newRecorder()
	sink.hold()
	reference, err := called.scope.exportSink(ctx, sink)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := called.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "closing", Steps: 16}, Progress: workerprotocol.Handle{Channel: reference},
	}); err != nil {
		t.Fatal(err)
	}
	sink.waitForReport(t)
	job := workers.worker.jobFor("closing")
	if job == nil {
		t.Fatal("the worker made no job")
	}
	_ = called.client.Close()

	eventually(t, "the job settles when its connection goes", func() bool {
		return job.Ending() == "lost" || job.Ending() == "unreachable"
	})
	if _, _, ended, _ := sink.snapshot(); ended != "" {
		t.Fatalf("a sink whose connection died was ended %q", ended)
	}

	// A new connection does not revive anything: it has its own scope, its
	// own table, and the number that named the old sink names nothing until
	// this connection mints it.
	fresh := dialWorkers(t, ctx, workers)
	if _, err := fresh.client.Start(ctx, workerprotocol.Start{
		Ticket: workerprotocol.Ticket{Label: "revived", Steps: 1}, Progress: workerprotocol.Handle{Channel: reference},
	}); !workerprotocol.IsError(err, workerprotocol.ErrorUnknownReference) {
		t.Fatalf("an id from a dead connection resolved on a new one: %v", err)
	}
}
