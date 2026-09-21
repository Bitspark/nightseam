package duplex

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// ProfileKind is one of the profile's four frame kinds. Correlation and
// validation remain the peer's; a wire only carries the frame.
type ProfileKind string

const (
	ProfileRequest  ProfileKind = "request"
	ProfileResponse ProfileKind = "response"
	ProfileEvent    ProfileKind = "event"
	ProfileCancel   ProfileKind = "cancel"
)

// ProfileError is public error data, without a runtime error dependency.
type ProfileError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// ProfileFrame carries a profile frame. The Send path is the request method or
// event name; keeping it outside this value prevents contradictory names.
// Payloads retain their JSON representation, including numeric precision.
type ProfileFrame struct {
	Version     int               `json:"version"`
	Kind        ProfileKind       `json:"kind"`
	ID          string            `json:"id,omitempty"`
	Params      json.RawMessage   `json:"params,omitempty"`
	Result      json.RawMessage   `json:"result,omitempty"`
	Error       *ProfileError     `json:"error,omitempty"`
	Data        json.RawMessage   `json:"data,omitempty"`
	Traceparent string            `json:"traceparent,omitempty"`
	Tracestate  string            `json:"tracestate,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

// ReturnAddress is a local address with stable pointer identity, even when its
// Wire implementation is not comparable. It is never an envelope member.
type ReturnAddress struct{ Wire Wire }

// Message preserves a frame and its local return capability through routing.
type Message struct {
	Frame  ProfileFrame
	Return *ReturnAddress `json:"-"`
}

// Receiver receives deliveries at its registered relative path, and an ending.
// A root owns asynchronous dispatch; composition does not invoke Message itself.
type Receiver struct {
	Message func(path []string, message Message)
	Closed  func(code Code, reason string)
}

// Wire is an endpoint with an origin. Receive registers an exact relative
// dispatch path; duplicate registrations are refused. Its detach is idempotent.
// Send returns when accepted or refused, without running a destination handler.
// A root owns queue bounds, dispatch and carrier closure. A selected view shares
// that ownership; a mount only owns its routing and registrations.
type Wire interface {
	Send(path []string, message Message) error
	Receive(path []string, receiver Receiver) (detach func(), err error)
	Close(code Code, reason string) error
}

var (
	ErrPath           = errors.New("invalid wire path")
	ErrNoRoute        = errors.New("wire path has no destination")
	ErrReceiverExists = errors.New("wire path already has a receiver")
)

// EncodePath concatenates UTF-8 byte-length-prefixed scalar-string segments.
// The empty path is "", while a single empty segment is "0:".
func EncodePath(path []string) (string, error) {
	var encoded strings.Builder
	for _, segment := range path {
		if !utf8.ValidString(segment) {
			return "", ErrPath
		}
		encoded.WriteString(strconv.Itoa(len(segment)))
		encoded.WriteByte(':')
		encoded.WriteString(segment)
	}
	return encoded.String(), nil
}

// DecodePath accepts only the canonical form of EncodePath, without Unicode
// normalization or interpretation of dots, slashes or empty segments.
func DecodePath(encoded string) ([]string, error) {
	path := []string{}
	for encoded != "" {
		colon := strings.IndexByte(encoded, ':')
		if colon <= 0 {
			return nil, ErrPath
		}
		digits := encoded[:colon]
		if len(digits) > 1 && digits[0] == '0' {
			return nil, ErrPath
		}
		for _, digit := range digits {
			if digit < '0' || digit > '9' {
				return nil, ErrPath
			}
		}
		length, err := strconv.ParseUint(digits, 10, 64)
		encoded = encoded[colon+1:]
		if err != nil || length > uint64(len(encoded)) {
			return nil, ErrPath
		}
		segment := encoded[:int(length)]
		if !utf8.ValidString(segment) {
			return nil, ErrPath
		}
		path = append(path, segment)
		encoded = encoded[int(length):]
	}
	return path, nil
}

type selectedWire struct {
	root   Wire
	prefix []string
}

// At selects a relative path without allocating a peer, channel or queue.
// Closing the selection closes the endpoint it selects from.
func At(root Wire, path []string) Wire {
	return &selectedWire{root: root, prefix: append([]string{}, path...)}
}

func (w *selectedWire) path(path []string) []string {
	return append(append([]string{}, w.prefix...), path...)
}
func (w *selectedWire) Send(path []string, message Message) error {
	return w.root.Send(w.path(path), message)
}
func (w *selectedWire) Receive(path []string, receiver Receiver) (func(), error) {
	return w.root.Receive(w.path(path), receiver)
}
func (w *selectedWire) Close(code Code, reason string) error { return w.root.Close(code, reason) }

type mountedWire struct {
	children      map[string]Wire
	mu            sync.Mutex
	closed        bool
	registrations map[*mountedReceiver]struct{}
}
type mountedReceiver struct {
	receiver Receiver
	detach   func()
	active   bool
}

// Mount consumes one path segment and delegates to that child. The map is
// copied. A mount has no leaf at []; [""] can select an empty-string key.
// Closing a mount detaches its receivers and leaves every child usable.
func Mount(children map[string]Wire) Wire {
	w := &mountedWire{children: make(map[string]Wire, len(children)), registrations: map[*mountedReceiver]struct{}{}}
	for key, child := range children {
		w.children[key] = child
	}
	return w
}

func (w *mountedWire) destination(path []string) (Wire, error) {
	if w.closed {
		return nil, ErrClosed
	}
	if len(path) == 0 {
		return nil, ErrNoRoute
	}
	if _, err := EncodePath(path); err != nil {
		return nil, err
	}
	child := w.children[path[0]]
	if child == nil {
		return nil, ErrNoRoute
	}
	return child, nil
}
func (w *mountedWire) Send(path []string, message Message) error {
	w.mu.Lock()
	child, err := w.destination(path)
	w.mu.Unlock()
	if err != nil {
		return err
	}
	return child.Send(append([]string{}, path[1:]...), message)
}
func (w *mountedWire) Receive(path []string, receiver Receiver) (func(), error) {
	w.mu.Lock()
	child, err := w.destination(path)
	if err != nil {
		w.mu.Unlock()
		return nil, err
	}
	registration := &mountedReceiver{receiver: receiver, active: true}
	w.registrations[registration] = struct{}{}
	w.mu.Unlock()
	detach, err := child.Receive(append([]string{}, path[1:]...), Receiver{
		Message: func(path []string, message Message) {
			w.mu.Lock()
			active := registration.active
			w.mu.Unlock()
			if active && receiver.Message != nil {
				receiver.Message(path, message)
			}
		},
		Closed: func(code Code, reason string) { w.remove(registration, true, code, reason) },
	})
	w.mu.Lock()
	active := registration.active
	if err != nil {
		registration.active = false
		delete(w.registrations, registration)
	} else if active {
		registration.detach = detach
	}
	w.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if !active {
		if detach != nil {
			detach()
		}
		return nil, ErrClosed
	}
	return func() { w.remove(registration, false, 0, "") }, nil
}
func (w *mountedWire) remove(registration *mountedReceiver, tell bool, code Code, reason string) {
	w.mu.Lock()
	if !registration.active {
		w.mu.Unlock()
		return
	}
	registration.active = false
	delete(w.registrations, registration)
	detach := registration.detach
	w.mu.Unlock()
	if detach != nil {
		detach()
	}
	if tell && registration.receiver.Closed != nil {
		registration.receiver.Closed(code, reason)
	}
}
func (w *mountedWire) Close(code Code, reason string) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	registrations := make([]*mountedReceiver, 0, len(w.registrations))
	for registration := range w.registrations {
		registration.active = false
		registrations = append(registrations, registration)
	}
	w.registrations = nil
	w.mu.Unlock()
	for _, registration := range registrations {
		if registration.detach != nil {
			registration.detach()
		}
	}
	for _, registration := range registrations {
		if registration.receiver.Closed != nil {
			registration.receiver.Closed(code, reason)
		}
	}
	return nil
}
