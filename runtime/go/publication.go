package runtime

// UnpublishedError reports a local refusal before a frame entered the peer's
// outbound queue. Its underlying cause retains the ordinary public error or
// cancellation identity. A queued write failure or a remote response never
// supplies this proof, even if it has the same error code or message.
//
// The proof belongs to this send attempt. A handler must not treat a nested
// call's refusal as proof that its own already-dispatched request was unsent.
type UnpublishedError struct {
	cause error
}

func (e *UnpublishedError) Error() string { return e.cause.Error() }

// Unwrap preserves errors.Is and errors.As for the original refusal.
func (e *UnpublishedError) Unwrap() error { return e.cause }

func unpublished(err error) error {
	if err == nil {
		return nil
	}
	return &UnpublishedError{cause: err}
}
