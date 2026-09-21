package runtime

import (
	"context"
	"encoding/json"
)

// ValueAdapter pairs a declaration with its two primitive conversions. A
// reusable adapter retains no operation context or lifetime. When NeedsContext
// is true, conversion runs inside the supplied ValueEnvironment's matching
// Export, Import or Publish boundary, using the active context it provides.
type ValueAdapter[T any] struct {
	Binding      TypeBinding
	NeedsContext bool
	Export       func(context.Context, T) (json.RawMessage, error)
	Import       func(context.Context, json.RawMessage) (T, error)
}

// ValueEnvironment supplies operation-local conversion effects. It belongs to
// the consumer's chosen component; the runtime never interprets its context.
// Select chooses an outgoing context, and Child derives an incoming lifetime.
// Build callbacks are synchronous and must pass their active context through
// every nested conversion. Publish completes the build before attempting send.
type ValueEnvironment interface {
	Select(context.Context) (context.Context, error)
	Child(context.Context) (context.Context, error)
	Export(context.Context, func(context.Context) (json.RawMessage, error)) (json.RawMessage, error)
	Import(context.Context, func(context.Context) error) error
	Publish(context.Context, func(context.Context) (json.RawMessage, error), func(json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error)
}

// JSONAdapter validates ordinary data in both directions. It neither consults
// the context nor requires a conversion environment.
func JSONAdapter[T any]() ValueAdapter[T] {
	binding := TypeArgument[T]()
	return ValueAdapter[T]{
		Binding: binding,
		Export: func(_ context.Context, value T) (json.RawMessage, error) {
			raw, err := MarshalJSON(value)
			if err == nil {
				err = binding.Schema.ValidateExpressionRaw(binding.Type, raw)
			}
			return raw, err
		},
		Import: func(_ context.Context, raw json.RawMessage) (T, error) {
			var value T
			if err := binding.Schema.ValidateExpressionRaw(binding.Type, raw); err != nil {
				return value, err
			}
			err := json.Unmarshal(raw, &value)
			return value, err
		},
	}
}
