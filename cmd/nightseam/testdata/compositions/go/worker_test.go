package composition_test

// The application. A worker takes the sink a caller has already served, keeps
// it past the call that brought it, and answers with a job it serves itself.
// Every behavioral promise a reader might otherwise assume — that reports
// arrive in order, that the sink is ended exactly once, that cancelling the
// job is neither cancelling a call nor releasing a reference — is made here,
// in this file, by this code. None of it follows from a callable having been
// returned.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	jobprotocol "example.test/generated/api/go/job-protocol"
	sinkprotocol "example.test/generated/api/go/sink-protocol"
	workerprotocol "example.test/generated/api/go/worker-protocol"
	runtime "github.com/Bitspark/nightseam/runtime/go"
)

// theWorker is the worker family's server side. One implementation serves
// every connection, and it keeps a scope per connection: a live context is
// bound to one outer connection, so two callers' references are in different
// tables and a number that means one thing to one caller means nothing, or
// something else, to the other.
type theWorker struct {
	mu     sync.Mutex
	scopes map[*runtime.Peer]*scope
	jobs   []*theJob

	// origin is the worker this one forwards into, when a test has given it
	// one, and the scope of the second connection it reaches that worker over.
	origin      workerprotocol.ServerMethods
	originScope *scope
	forwarded   *relay

	slows atomic.Int64
}

type workerSession struct {
	*theWorker
	here *scope
}

var _ workerprotocol.ServerMethods = (*workerSession)(nil)

func newWorker() *theWorker { return &theWorker{scopes: map[*runtime.Peer]*scope{}} }

// attach gives a connection its scope, in Prepare, before the peer reads.
func (w *theWorker) attach(peer *runtime.Peer, s *scope) {
	w.mu.Lock()
	w.scopes[peer] = s
	w.mu.Unlock()
	go func() {
		<-peer.Done()
		// The scope ends with the connection: every binding it served and
		// every attachment it held goes, and nothing resolves in it again.
		s.close(context.Background())
		w.mu.Lock()
		delete(w.scopes, peer)
		w.mu.Unlock()
	}()
}

// Start is the whole of #196's example. It resolves the reference it was
// given, serves a job of its own, answers with that job's reference, and only
// then — after this call has returned — begins reporting through the sink.
func (w *workerSession) Start(ctx context.Context, params workerprotocol.Start) (workerprotocol.Started, error) {
	here := w.here
	sink, err := here.importSink(ctx, params.Progress.Channel)
	if err != nil {
		return workerprotocol.Started{}, &runtime.PublicError{Code: workerprotocol.ErrorUnknownReference, Message: err.Error()}
	}
	job := &theJob{
		label:  params.Ticket.Label,
		steps:  params.Ticket.Steps,
		sink:   sink,
		cancel: make(chan struct{}),
		done:   make(chan struct{}),
		state:  "starting",
	}
	id, err := here.exportJob(ctx, job)
	if err != nil {
		return workerprotocol.Started{}, err
	}
	job.self = id
	w.mu.Lock()
	w.jobs = append(w.jobs, job)
	w.mu.Unlock()
	// The connection's context, not the request's: the reference outlives the
	// call that introduced it, and so does the work that uses it.
	go job.run(here.context())
	return workerprotocol.Started{Job: workerprotocol.Handle{Channel: id}, Accepted: true}, nil
}

// jobFor is the job this worker made for a ticket, for the tests to read.
func (w *theWorker) jobFor(label string) *theJob {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, job := range w.jobs {
		if job.label == label {
			return job
		}
	}
	return nil
}

// Collect resolves every reference in the message, wherever the language
// admits one, and says how many resolved and how many were refused.
func (w *workerSession) Collect(ctx context.Context, params workerprotocol.Held) (workerprotocol.Counted, error) {
	here := w.here
	counted := workerprotocol.Counted{}
	resolve := func(handle workerprotocol.Handle) {
		if _, err := here.importSink(ctx, handle.Channel); err != nil {
			counted.Refused++
			return
		}
		counted.Resolved++
	}
	resolve(params.One)
	for _, handle := range params.Many {
		resolve(handle)
	}
	for _, handle := range params.ByName {
		resolve(handle)
	}
	if params.Maybe.Present && !params.Maybe.Value.Null {
		resolve(params.Maybe.Value.Value)
	}
	if params.Either.Kind() == workerprotocol.EitherKindSink {
		resolve(*params.Either.Sink)
	}
	resolve(params.Inline.Held)
	return counted, nil
}

// Forward re-exports an imported reference into another connection's scope
// and has the worker there report through it. The re-export is a binding of
// its own on that second connection, with its own lifetime: releasing it
// leaves the origin binding alone, and the origin closing reaches it as a
// failed call rather than as silence.
func (w *workerSession) Forward(ctx context.Context, params workerprotocol.Forward) (workerprotocol.Counted, error) {
	here := w.here
	if w.origin == nil || w.originScope == nil {
		return workerprotocol.Counted{}, &runtime.PublicError{Code: workerprotocol.ErrorNoOrigin, Message: "this worker forwards into nothing"}
	}
	origin, err := here.importSink(ctx, params.Progress.Channel)
	if err != nil {
		return workerprotocol.Counted{}, &runtime.PublicError{Code: workerprotocol.ErrorUnknownReference, Message: err.Error()}
	}
	relayed := &relay{to: origin}
	id, err := w.originScope.exportSink(w.originScope.context(), relayed)
	if err != nil {
		return workerprotocol.Counted{}, err
	}
	relayed.self = id
	w.mu.Lock()
	w.forwarded = relayed
	w.mu.Unlock()
	if _, err := w.origin.Start(ctx, workerprotocol.Start{Ticket: params.Ticket, Progress: workerprotocol.Handle{Channel: id}}); err != nil {
		return workerprotocol.Counted{}, err
	}
	return workerprotocol.Counted{Resolved: 1}, nil
}

// Slow answers nothing until its own call is cancelled, which is how
// cancelling a call is told apart from releasing a reference and from
// cancelling the application's job.
func (w *theWorker) Slow(ctx context.Context, _ workerprotocol.Ticket) (string, error) {
	w.slows.Add(1)
	<-ctx.Done()
	return "", ctx.Err()
}

// relay is a sink that forwards what it is told to a sink of another
// connection. It is the whole of explicit forwarding: an implementation like
// any other, exported into the scope it is to be reachable in.
type relay struct {
	to   sinkprotocol.ClientMethods
	self int64

	mu      sync.Mutex
	relayed int64
	failed  error
}

var _ sinkprotocol.ClientMethods = (*relay)(nil)

func (r *relay) Report(ctx context.Context, params sinkprotocol.ReportRequest) (int64, error) {
	taken, err := r.to.Report(ctx, params)
	r.mu.Lock()
	if err != nil {
		r.failed = err
	} else {
		r.relayed++
	}
	r.mu.Unlock()
	if err != nil {
		return 0, &runtime.PublicError{Code: "origin_gone", Message: err.Error()}
	}
	return taken, nil
}

func (r *relay) End(ctx context.Context, params sinkprotocol.Ending) (int64, error) {
	return r.to.End(ctx, params)
}

func (r *relay) counts() (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.relayed, r.failed
}

// theJob is the returned callable: an interface of two operations over state
// the application owns. Its contract, stated here and tested:
//
//   - ordering: one report is in flight at a time and each is awaited before
//     the next is sent, so the sink sees 1, 2, 3 … and the worker is paced by
//     the sink's answers;
//   - completion: exactly one End reaches the sink — done, failed or
//     cancelled — and no Report follows it;
//   - cancellation: Cancel asks the application to stop; it ends the sink
//     with cancelled and answers what had been delivered. It is not the
//     cancellation of any call, and it is not the release of any reference;
//   - the completion race, which #202 asks to be specified rather than left
//     open: a Cancel that arrives while the last report is in flight loses.
//     The job ends once, whichever reason got there first, and Cancel says
//     which — `stopped` is true only where the cancellation is what ended it.
//     A caller that must know reads the answer rather than assuming.
type theJob struct {
	label string
	steps int64
	sink  sinkprotocol.ClientMethods
	// self is the reference this job was published under, which is what the
	// forwarding case revokes.
	self int64

	cancel chan struct{}
	done   chan struct{}

	once      sync.Once
	mu        sync.Mutex
	delivered int64
	state     string
	ending    string
}

var _ jobprotocol.ServerMethods = (*theJob)(nil)

func (j *theJob) run(ctx context.Context) {
	defer close(j.done)
	j.setState("running")
	for step := int64(1); step <= j.steps; step++ {
		select {
		case <-j.cancel:
			j.end(ctx, "cancelled", sinkprotocol.Ending{Cancelled: &struct{}{}})
			return
		case <-ctx.Done():
			j.setState("gone")
			return
		default:
		}
		percent := step * 100 / j.steps
		taken, err := j.sink.Report(ctx, sinkprotocol.ReportRequest{
			Sequence: step,
			Item:     sinkprotocol.Item{Progress: &sinkprotocol.Progress{Percent: percent}},
		})
		if err != nil {
			// The reference died under us — released, revoked, or its
			// connection gone. The job ends; nothing is retried, and the
			// application decides that, not the wire.
			j.setState("lost")
			j.setEnding("lost")
			return
		}
		j.mu.Lock()
		j.delivered = taken
		j.mu.Unlock()
	}
	j.end(ctx, "done", sinkprotocol.Ending{Done: &sinkprotocol.Done{Delivered: j.Delivered()}})
}

func (j *theJob) end(ctx context.Context, state string, ending sinkprotocol.Ending) {
	j.once.Do(func() {
		j.setState(state)
		if _, err := j.sink.End(ctx, ending); err != nil {
			j.setEnding("unreachable")
			return
		}
		switch {
		case ending.Cancelled != nil:
			j.setEnding("cancelled")
		case ending.Done != nil:
			j.setEnding("done")
		default:
			j.setEnding("failed")
		}
	})
}

func (j *theJob) setState(state string) {
	j.mu.Lock()
	j.state = state
	j.mu.Unlock()
}

func (j *theJob) setEnding(ending string) {
	j.mu.Lock()
	j.ending = ending
	j.mu.Unlock()
}

// Delivered is how many reports the sink has taken.
func (j *theJob) Delivered() int64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.delivered
}

// Ending is how the sink was ended, once it has been.
func (j *theJob) Ending() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.ending
}

func (j *theJob) Status(_ context.Context) (jobprotocol.Status, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return jobprotocol.Status{State: j.state, Delivered: j.delivered}, nil
}

func (j *theJob) Cancel(_ context.Context) (jobprotocol.Cancelled, error) {
	select {
	case <-j.done:
		return jobprotocol.Cancelled{}, &runtime.PublicError{Code: jobprotocol.ErrorJobFinished, Message: fmt.Sprintf("%s had ended", j.label)}
	default:
	}
	select {
	case <-j.cancel:
	default:
		close(j.cancel)
	}
	<-j.done
	// The race, resolved where it is visible: the job ends once, and this
	// answer says whether the cancellation is what ended it.
	return jobprotocol.Cancelled{Stopped: j.Ending() == "cancelled", Delivered: j.Delivered()}, nil
}
