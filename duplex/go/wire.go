package duplex

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	bitwire "github.com/Bitspark/bitwire/wire/go"
)

// ProfileKind is one of the profile's four frame kinds. Correlation and
// validation remain the peer's; a wire only carries the frame.
type ProfileKind = bitwire.ProfileKind

const (
	ProfileRequest  = bitwire.ProfileRequest
	ProfileResponse = bitwire.ProfileResponse
	ProfileEvent    = bitwire.ProfileEvent
	ProfileCancel   = bitwire.ProfileCancel
)

// ProfileError is public error data, without a runtime error dependency.
type ProfileError = bitwire.ProfileError

// ProfileFrame carries a profile frame. The Send path is the request method or
// event name; keeping it outside this value prevents contradictory names.
// Payloads retain their JSON representation, including numeric precision.
type ProfileFrame = bitwire.ProfileFrame

// ReturnAddress is a local address with stable pointer identity, even when its
// Wire implementation is not comparable. It is never an envelope member.
type ReturnAddress = bitwire.ReturnAddress

// Message preserves a frame and its local return capability through routing.
type Message = bitwire.Message

// Receiver receives deliveries relative to its wire's origin, and an ending.
// A root owns asynchronous dispatch; composition does not invoke Message itself.
type Receiver = bitwire.Receiver

// Wire is an endpoint with an origin. Receive registers an exact relative
// dispatch path; duplicate registrations are refused. Its detach is idempotent.
// Send returns when accepted or refused, without running a destination handler.
// A root owns queue bounds, dispatch and carrier closure. A selected view shares
// that ownership; a mount only owns its routing and registrations.
// It is the shared Bitwire type; this package supplies its Nightseam views.
type Wire = bitwire.Wire

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
	return w.root.Receive(w.path(path), Receiver{
		Namespace: receiver.Namespace,
		Message: func(delivered []string, message Message) {
			if receiver.Message != nil {
				receiver.Message(append([]string{}, delivered[len(w.prefix):]...), message)
			}
		},
		Closed: receiver.Closed,
	})
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
	if len(path) == 0 && receiver.Namespace {
		return w.receiveNamespace(receiver)
	}
	w.mu.Lock()
	child, err := w.destination(path)
	if err != nil {
		w.mu.Unlock()
		return nil, err
	}
	registration := &mountedReceiver{receiver: receiver, active: true}
	key := path[0]
	w.registrations[registration] = struct{}{}
	w.mu.Unlock()
	detach, err := child.Receive(append([]string{}, path[1:]...), Receiver{
		Namespace: receiver.Namespace,
		Message: func(path []string, message Message) {
			// Detach removes future dispatch at the child. Already accepted
			// requests retain their captured receiver for cancellation.
			if receiver.Message != nil {
				receiver.Message(append([]string{key}, path...), message)
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

// A namespace at the mount origin receives every child under that child's
// key. One child's end removes only that route; the namespace ends when its
// last child ends or when the mount itself is closed.
func (w *mountedWire) receiveNamespace(receiver Receiver) (func(), error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, ErrClosed
	}
	keys := make([]string, 0, len(w.children))
	for key, child := range w.children {
		if child != nil {
			keys = append(keys, key)
		}
	}
	var mu sync.Mutex
	var detaches []func()
	ended, remaining := false, len(keys)
	registration := &mountedReceiver{receiver: receiver, active: true}
	registration.detach = func() {
		mu.Lock()
		ended = true
		held := detaches
		detaches = nil
		mu.Unlock()
		for _, detach := range held {
			detach()
		}
	}
	w.registrations[registration] = struct{}{}
	w.mu.Unlock()
	for _, key := range keys {
		detach, err := w.Receive([]string{key}, Receiver{
			Namespace: true,
			Message:   receiver.Message,
			Closed: func(code Code, reason string) {
				mu.Lock()
				remaining--
				last := remaining == 0
				mu.Unlock()
				if last {
					w.remove(registration, true, code, reason)
				}
			},
		})
		if err != nil {
			w.remove(registration, false, 0, "")
			return nil, err
		}
		mu.Lock()
		if ended {
			mu.Unlock()
			detach()
			return nil, ErrClosed
		}
		detaches = append(detaches, detach)
		mu.Unlock()
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
