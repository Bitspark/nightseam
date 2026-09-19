package session

import "fmt"

// The codes a session refuses with. They are the layer's own vocabulary and
// the same ten names in both languages, so a consumer's server branches on
// the same fact wherever it runs: the code is what a program reads, the
// message what a person does. A call of this package refuses with an *Error
// carrying one; ErrorNotControlling and ErrorBusy are also what the relay
// answers a consumer's frame with on the wire, which is why they are the two
// a consumer meets as the profile's error object rather than as a return.
const (
	// ErrorInvalidOptions: what a call was given is not what it takes — a
	// limit that is not a limit, a replay with nowhere to deliver.
	ErrorInvalidOptions = "invalid_options"
	// ErrorNoSession: no session is bound under that id, or the one that was
	// has ended.
	ErrorNoSession = "no_session"
	// ErrorNotAttached: control was given to a consumer that is not attached
	// to this session.
	ErrorNotAttached = "not_attached"
	// ErrorNotControlling: an observer was given control, or — on the wire —
	// a deciding frame came from a consumer that does not hold it.
	ErrorNotControlling = "not_controlling"
	// ErrorOriginInvalid: an origin is the caller's fact about the consumer,
	// as Unicode scalar text.
	ErrorOriginInvalid = "origin_invalid"
	// ErrorRoleInvalid: a consumer attaches as a participant or an observer,
	// and as nothing else.
	ErrorRoleInvalid = "role_invalid"
	// ErrorSequenceInvalid: a consumer resumes from a sequence, which is zero
	// or more.
	ErrorSequenceInvalid = "sequence_invalid"
	// ErrorSessionExists: a session is already bound under that id.
	ErrorSessionExists = "session_exists"
	// ErrorSessionInvalid: a session is bound under an id, over a connection,
	// with its family's governance and a log, and a consumer attaches over a
	// connection.
	ErrorSessionInvalid = "session_invalid"
	// ErrorTooManyAttachments: no room for another consumer on this session.
	ErrorTooManyAttachments = "too_many_attachments"
)

// ErrorBusy is the refusal no call returns: the relay answers a consumer's
// request with it where the session already has as many open towards the
// machine as it may. It is the session's vocabulary all the same, and is
// here beside the ten so that one place names every code this layer speaks.
const ErrorBusy = "busy"

// Error is what a session call refuses with: a code a program branches on
// and a message a person reads. Every refusal New, Bind, Attach, Control and
// the package's own Log return is one, so both of the ways Go asks about an
// error reach it —
//
//	var refusal *session.Error
//	if errors.As(err, &refusal) && refusal.Code == session.ErrorNoSession { … }
//
//	if errors.Is(err, &session.Error{Code: session.ErrorNoSession}) { … }
//
// — the second because Is below matches by code alone, a message being the
// part of a refusal that may be reworded.
//
// It is not runtime.PublicError: that is what crosses the wire as a
// response, and these are what a call in this process returns. The two
// codes that are both travel as a PublicError on the wire and as an *Error
// from a call, under the same name.
type Error struct {
	// Code is the refusal, one of the constants above.
	Code string
	// Message says what happened, for a person.
	Message string
}

// Error is the code and the message, as a log line shows them.
func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Is reports whether target is a refusal of the same code, which is what
// errors.Is against a bare *Error asks.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && other.Code == e.Code
}

// coded is one refusal, the message written as an error's is.
func coded(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}
