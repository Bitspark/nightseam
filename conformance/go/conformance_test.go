package conformance

import (
	"os"
	"testing"
)

// TestSelf holds the Go testee to every scenario on both sides of the
// wire: the reference passes its own suite before anyone is held to it.
func TestSelf(t *testing.T) {
	s := Open(t)
	s.pair(t, "go", "go")
}

// TestStar holds every other language to Go, on either side in turn: the
// gate a language passes to have joined.
func TestStar(t *testing.T) {
	s := Open(t)
	for _, p := range s.Pairings(false) {
		if p[0] == "go" && p[1] == "go" {
			continue
		}
		t.Run(pairName(p[0], p[1]), func(t *testing.T) { s.pair(t, p[0], p[1]) })
	}
}

// TestMatrix holds every language to every other, with NIGHTSEAM_MATRIX
// set: what two languages disagree on that each agrees with Go about.
func TestMatrix(t *testing.T) {
	if os.Getenv("NIGHTSEAM_MATRIX") == "" {
		t.Skip("the full matrix runs with NIGHTSEAM_MATRIX=1")
	}
	s := Open(t)
	for _, p := range s.Pairings(true) {
		if p[0] == "go" || p[1] == "go" {
			continue
		}
		t.Run(pairName(p[0], p[1]), func(t *testing.T) { s.pair(t, p[0], p[1]) })
	}
}
