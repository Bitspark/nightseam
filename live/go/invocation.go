package live

import (
	"context"
	"encoding/json"

	"github.com/Bitspark/nightseam/runtime/go"
)

// invokeScoped separates the caller's lifetime from the implementation's.
// Closing the scope cancels the body and settles the caller, even if the body
// ignores cancellation. Its eventual result has no second route to the peer.
func (s *Scope) invokeScoped(ctx context.Context, invoke Invoke, request json.RawMessage) (json.RawMessage, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, &runtime.PublicError{Code: ErrorScopeClosed, Message: "the scope ended"}
	}
	call, cancel := context.WithCancel(ctx)
	ticket := s.call
	s.call++
	s.inflight[ticket] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.inflight, ticket)
		s.mu.Unlock()
		cancel()
	}()

	type outcome struct {
		value    json.RawMessage
		err      error
		panicked any
	}
	done := make(chan outcome, 1)
	go func() {
		var answer outcome
		defer func() {
			answer.panicked = recover()
			done <- answer
		}()
		if err := call.Err(); err != nil {
			answer.err = err
			return
		}
		answer.value, answer.err = invoke(call, request)
	}()
	var answer outcome
	select {
	case answer = <-done:
	case <-call.Done():
		answer.err = call.Err()
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed && ctx.Err() == nil {
		return nil, &runtime.PublicError{Code: ErrorScopeClosed, Message: "the scope ended"}
	}
	if answer.panicked != nil {
		// Keep the peer's existing panic reporting for a live incoming call,
		// and the caller's panic handling for a local one.
		panic(answer.panicked)
	}
	return answer.value, runtime.WithoutUnpublishedProof(answer.err)
}
