package duplex

import bitwire "github.com/Bitspark/bitwire/wire/go"

// DeclaredAdmissions reports how many admitted requests bound access still
// holds a cancellation route for.
func DeclaredAdmissions(access bitwire.Wire) int {
	w := access.(*declaredWire)
	w.mu.Lock()
	defer w.mu.Unlock()
	held := 0
	for _, requests := range w.admitted {
		held += len(requests)
	}
	return held
}
