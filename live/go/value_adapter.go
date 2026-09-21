package live

import (
	"encoding/json"

	"github.com/Bitspark/nightseam/runtime/go"
)

// AdapterContext supplies the explicit lifetime environment of a generated
// interpretation. Reusable value adapters receive the active owner at each use.
type AdapterContext struct {
	runtime.AdapterContext
	Scope *Scope
	Owner *Owner
}

// ValueAdapter keeps a value's declaration and both boundary conversions
// together. Live reports whether conversion can acquire bindings. Conversion
// receives the current owner, including its active publication batch; factories
// retain adapters, never a permanent owner.
type ValueAdapter[T any] struct {
	Binding runtime.TypeBinding
	Live    bool
	Export  func(*Owner, T) (json.RawMessage, error)
	Import  func(*Owner, json.RawMessage) (T, error)
}

// JSONAdapter describes ordinary Go data using its existing runtime binding.
// It validates both directions and needs no owner, including after release.
func JSONAdapter[T any]() ValueAdapter[T] {
	binding := runtime.TypeArgument[T]()
	return ValueAdapter[T]{
		Binding: binding,
		Export: func(_ *Owner, value T) (json.RawMessage, error) {
			raw, err := runtime.MarshalJSON(value)
			if err == nil {
				err = binding.Schema.ValidateExpressionRaw(binding.Type, raw)
			}
			return raw, err
		},
		Import: func(_ *Owner, raw json.RawMessage) (T, error) {
			var value T
			if err := binding.Schema.ValidateExpressionRaw(binding.Type, raw); err != nil {
				return value, err
			}
			err := json.Unmarshal(raw, &value)
			return value, err
		},
	}
}
