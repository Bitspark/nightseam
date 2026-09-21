package conformance

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestMain writes the matrix once every test that opened a suite is done.
func TestMain(m *testing.M) {
	code := m.Run()
	RemoveOut()
	if err := WriteMatrix(); err != nil {
		fmt.Fprintln(os.Stderr, "write matrix.json:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// TestSelf holds the Go testee to every scenario on both sides of the
// wire: the reference passes its own suite before anyone is held to it.
func TestSelf(t *testing.T) {
	s := Open(t)
	s.pair(t, "go", "go")
}

// TestStar holds every other language to Go, on either side in turn: the
// gate holds the tier's promise; nonblocking failures remain in the matrix.
func TestStar(t *testing.T) {
	s := Open(t)
	for _, p := range s.Pairings(false) {
		if p[0] == "go" && p[1] == "go" {
			continue
		}
		t.Run(pairName(p[0], p[1]), func(t *testing.T) { s.pair(t, p[0], p[1]) })
	}
}

// TestDialOnly is the proof that a language whose runtime only dials is
// held in every role: the star with the TypeScript testee treated as
// unable to listen, so every scenario of the seam, the peer and the tunnel
// runs over a connection the Go side accepted — and none
// may be skipped for it. Its outcomes go to a matrix of their own, since
// the language's row counts each scenario once.
func TestDialOnly(t *testing.T) {
	t.Setenv("NIGHTSEAM_PRETEND_DIAL_ONLY", "typescript")
	s := Open(t)
	if _, ok := s.Recipes["typescript"]; !ok {
		t.Skip("no TypeScript testee")
	}
	// This deliberately incomplete testee does not describe the released
	// runtime. Keep its evidence separate and restore the release matrix
	// before Open's cleanup checks the real languages' coverage.
	releaseMatrix := s.Matrix
	s.Matrix = NewMatrix(s.Profiles)
	t.Cleanup(func() { s.Matrix = releaseMatrix })
	// The one skip allowed is a scenario that is about listening — one whose
	// file declares listen, a WebSocket handshake say — with the dial-only
	// testee on the side that listens; every other skip is the runner
	// failing to open the connection from the other side.
	s.Observe = func(sc Scenario, a, b string, o Outcome) {
		if o.Skipped == "" {
			return
		}
		if !slices.Contains(sc.Needs, "listen") || !strings.Contains(o.Skipped, "listen") {
			t.Errorf("%s (a: %s, b: %s) was skipped for a testee that only dials: %s", sc.Name, a, b, o.Skipped)
		}
	}
	for _, p := range [][2]string{{"go", "typescript"}, {"typescript", "go"}} {
		t.Run(pairName(p[0], p[1]), func(t *testing.T) { s.pair(t, p[0], p[1]) })
	}
	for profile, cell := range s.Matrix.rows["typescript"] {
		if cell.Passed == 0 {
			t.Errorf("%s: nothing passed", profile)
		}
	}
	if got := s.Matrix.Verdict(s.Profiles, "typescript"); got != "blocking" {
		t.Errorf("the artificial testee's required listening skip: verdict %s, want blocking", got)
	}
}

// TestGenerated holds every language's generated testee — what its target
// renders for the probe and proof families, over its runtime — to Go's, on either side,
// and Go's to its own.
func TestGenerated(t *testing.T) {
	s := Open(t)
	languages := s.PrepareGenerated(t)
	for _, a := range languages {
		for _, b := range languages {
			if a != "go" && b != "go" {
				continue
			}
			t.Run(pairName(a, b), func(t *testing.T) { s.runGenerated(t, a, b) })
		}
	}
}

// TestMatrix holds every language to every other, with NIGHTSEAM_MATRIX
// set: what two languages disagree on that each agrees with Go about.
func TestMatrix(t *testing.T) {
	if os.Getenv("NIGHTSEAM_MATRIX") == "" {
		t.Skip("the full matrix runs with NIGHTSEAM_MATRIX=1")
	}
	s := Open(t)
	s.strict = true
	for _, p := range s.Pairings(true) {
		if p[0] == "go" || p[1] == "go" {
			continue
		}
		t.Run(pairName(p[0], p[1]), func(t *testing.T) { s.pair(t, p[0], p[1]) })
	}
}
