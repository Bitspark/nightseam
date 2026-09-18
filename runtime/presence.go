package wsruntime

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

func Some[T any](value T) Optional[T] { return Optional[T]{Value: value, Present: true} }
func (v Optional[T]) IsZero() bool    { return !v.Present }
func (v Optional[T]) MarshalJSON() ([]byte, error) {
	if !v.Present {
		return []byte("null"), nil
	}
	return json.Marshal(v.Value)
}
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

func Null[T any]() Nullable[T]           { return Nullable[T]{Null: true} }
func NonNull[T any](value T) Nullable[T] { return Nullable[T]{Value: value} }
func (v Nullable[T]) MarshalJSON() ([]byte, error) {
	if v.Null {
		return []byte("null"), nil
	}
	return json.Marshal(v.Value)
}
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
