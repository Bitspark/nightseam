package runtime

import (
	"bytes"
	"encoding/json"
)

// Optional distinguishes an absent member from a present value. Generated Go
// fields use the omitzero JSON option; a standalone absent value encodes as null.
type Optional[T any] struct {
	Value   T
	Present bool
}

// Some is a present value.
func Some[T any](value T) Optional[T] { return Optional[T]{Value: value, Present: true} }

// IsZero reports absence, which is what omitzero reads.
func (v Optional[T]) IsZero() bool { return !v.Present }

// MarshalJSON writes the value, or null when absent; omitzero is what keeps
// an absent member off the wire.
func (v Optional[T]) MarshalJSON() ([]byte, error) {
	if !v.Present {
		return []byte("null"), nil
	}
	return json.Marshal(v.Value)
}

// UnmarshalJSON reads a member that is present; an absent one never reaches it.
func (v *Optional[T]) UnmarshalJSON(data []byte) error {
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value, v.Present = value, true
	return nil
}

// Nullable distinguishes JSON null from a non-null value. Use
// Optional[Nullable[T]] for an optional nullable field.
type Nullable[T any] struct {
	Value T
	Null  bool
}

// Null is JSON null.
func Null[T any]() Nullable[T] { return Nullable[T]{Null: true} }

// NonNull is a value that is not null.
func NonNull[T any](value T) Nullable[T] { return Nullable[T]{Value: value} }

// MarshalJSON writes the value, or null.
func (v Nullable[T]) MarshalJSON() ([]byte, error) {
	if v.Null {
		return []byte("null"), nil
	}
	return json.Marshal(v.Value)
}

// UnmarshalJSON reads null as Null and anything else as the value.
func (v *Nullable[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		var zero T
		v.Value, v.Null = zero, true
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value, v.Null = value, false
	return nil
}
