package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Bitspark/nightseam/runtime/go"
)

// message is one frame of the profile as a relay sees it: a JSON object it
// reads three members of and keeps every member of. A relay rewrites id and
// nothing else, so a member it does not know — a trace context, a member of
// a profile later than this one — reaches the other side as it arrived, in
// the place it arrived in.
type message struct {
	names  []string
	values map[string]json.RawMessage
}

// The reasons text that is no message of the profile is refused with. They
// are a closed set rather than a wording: the connection the frame arrived
// on is closed with one of them and a consumer reads it off the close, so
// both runtimes name the same fault in the same words — which an error in
// encoding/json's own, for a frame malformed inside, would not be.
const (
	notAnObject     = "a session frame must be a JSON object"
	trailingContent = "invalid trailing session frame content"
	repeatedMember  = "duplicate session frame member %q"
)

// decodeMessage reads a frame's members in the order they were written; a
// frame that is not an object, or repeats a member, or carries anything
// after the object, is no frame. What it refuses with is internal and
// reaches no caller of this package: it is the reason the connection the
// frame arrived on is closed with, a protocol error, which is why it
// carries no code of the session's vocabulary.
func decodeMessage(data []byte) (*message, error) {
	if err := runtime.ValidateUnicodeJSON(data); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New(notAnObject)
	}
	m := &message{values: map[string]json.RawMessage{}}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, errors.New(notAnObject)
		}
		name, ok := key.(string)
		if !ok {
			return nil, errors.New(notAnObject)
		}
		if _, exists := m.values[name]; exists {
			return nil, fmt.Errorf(repeatedMember, name)
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, errors.New(notAnObject)
		}
		m.names = append(m.names, name)
		m.values[name] = value
	}
	if _, err := d.Token(); err != nil {
		return nil, errors.New(notAnObject)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New(trailingContent)
	}
	return m, nil
}

// text is a member's value as a string, and "" where the member is absent
// or is not one; the relay reads kind, id and method and nothing else.
func (m *message) text(name string) string {
	raw, ok := m.values[name]
	if !ok {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

func (m *message) kind() string   { return m.text("kind") }
func (m *message) id() string     { return m.text("id") }
func (m *message) method() string { return m.text("method") }

// named is what the frame names: a request's method, an event's name. A
// response and a cancel name nothing, a response's method being no member
// of the wire.
func (m *message) named() string {
	if event := m.text("event"); event != "" {
		return event
	}
	return m.method()
}

// trace is the W3C members the frame carries, verbatim and unread: the
// relay forwards them and an observer is told what they were.
func (m *message) trace() runtime.Trace {
	return runtime.Trace{Parent: m.text("traceparent"), State: m.text("tracestate")}
}

// withID writes the frame again with its id replaced and every other member
// verbatim, in its place; a frame that carried none gains it at the end.
func (m *message) withID(id string) []byte {
	encoded, err := runtime.MarshalJSON(id)
	if err != nil {
		return nil
	}
	names := m.names
	if _, carried := m.values["id"]; !carried {
		names = append(append([]string{}, names...), "id")
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			out.WriteByte(',')
		}
		key, err := runtime.MarshalJSON(name)
		if err != nil {
			return nil
		}
		out.Write(key)
		out.WriteByte(':')
		if name == "id" {
			out.Write(encoded)
			continue
		}
		out.Write(m.values[name])
	}
	out.WriteByte('}')
	return out.Bytes()
}
