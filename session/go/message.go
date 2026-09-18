package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// decodeMessage reads a frame's members in the order they were written; a
// frame that is not an object, or repeats a member, is no frame.
func decodeMessage(data []byte) (*message, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("a session frame must be a JSON object")
	}
	m := &message{values: map[string]json.RawMessage{}}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, err
		}
		name, ok := key.(string)
		if !ok {
			return nil, errors.New("invalid session frame member name")
		}
		if _, exists := m.values[name]; exists {
			return nil, fmt.Errorf("duplicate session frame member %q", name)
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, err
		}
		m.names = append(m.names, name)
		m.values[name] = value
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid trailing session frame content")
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

// withID writes the frame again with its id replaced and every other member
// verbatim, in its place; a frame that carried none gains it at the end.
func (m *message) withID(id string) []byte {
	encoded, err := json.Marshal(id)
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
		key, err := json.Marshal(name)
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
