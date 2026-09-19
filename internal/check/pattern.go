package check

import "github.com/Bitspark/nightseam/internal/pattern"

// Dialect is the declaration and runtime descriptor's pattern language.
const Dialect = pattern.Dialect

// Pattern holds a declaration's field constraint to the same syntax guard
// used when a runtime reads a handwritten descriptor.
func Pattern(value string) error { return pattern.Check(value) }
