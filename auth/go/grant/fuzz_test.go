package grant_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Bitspark/nightseam/auth/go/grant"
)

// seedBodies and seedEnvelopes are the table's bytes, the corpus every
// fuzz target starts from.
func seedBodies(t testing.TB) [][]byte {
	t.Helper()
	tb := loadTable(t)
	var bodies [][]byte
	for _, raw := range tb.Cases {
		var c struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		if c.Body != "" {
			bodies = append(bodies, unhex(t, c.Body))
		}
	}
	return bodies
}

func seedEnvelopes(t testing.TB) [][]byte {
	t.Helper()
	tb := loadTable(t)
	var envelopes [][]byte
	for _, env := range tb.Envelopes {
		envelopes = append(envelopes, unhex(t, env))
	}
	return envelopes
}

// FuzzDecode holds Decode total: no input panics, a body it accepts
// re-encodes to itself — the encoding is canonical — and one it refuses
// answers a code of the closed set.
func FuzzDecode(f *testing.F) {
	for _, body := range seedBodies(f) {
		f.Add(body)
	}
	f.Add([]byte{})
	f.Add([]byte{0x01})
	f.Add([]byte{0x02, 0x00})
	f.Fuzz(func(t *testing.T, body []byte) {
		g, code := grant.Decode(body)
		switch code {
		case "":
			again, err := grant.Encode(g)
			if err != nil {
				t.Fatalf("a decoded body does not encode: %v", err)
			}
			if !bytes.Equal(again, body) {
				t.Fatalf("decoded %x re-encodes as %x", body, again)
			}
		case grant.Malformed, grant.UnsupportedVersion:
		default:
			t.Fatalf("Decode answered %q", code)
		}
	})
}

// FuzzOpen holds Open total: no input panics, a refused envelope names no
// issuer and no grant, and an opened one carries a body Decode accepts.
func FuzzOpen(f *testing.F) {
	for _, env := range seedEnvelopes(f) {
		f.Add(env)
	}
	f.Add([]byte{})
	f.Add([]byte("arcn"))
	f.Add(append([]byte("arcn\x01\x11nightseam-grant/1"), make([]byte, 96)...))
	f.Fuzz(func(t *testing.T, env []byte) {
		issuer, g, code := grant.Open(env)
		switch code {
		case "":
			if _, err := grant.Encode(g); err != nil {
				t.Fatalf("an opened grant does not encode: %v", err)
			}
		case grant.EnvelopeInvalid, grant.Malformed, grant.UnsupportedVersion:
			if issuer != [32]byte{} || g.Domain != "" || g.Scope != nil {
				t.Fatal("a refused envelope handed back an issuer or a grant")
			}
		default:
			t.Fatalf("Open answered %q", code)
		}
		grant.Digest(env)
	})
}

// FuzzVerify holds the evaluation total over an arbitrary envelope placed
// in a chain of the table's, at either end.
func FuzzVerify(f *testing.F) {
	tb := loadTable(f)
	for _, env := range seedEnvelopes(f) {
		f.Add(env, true)
		f.Add(env, false)
	}
	root := tb.root(f, jsonRoot{Key: "root", Domain: "example.test"})
	rootAlice, aliceBob := tb.envelope(f, "root_alice"), tb.envelope(f, "alice_bob")
	f.Fuzz(func(t *testing.T, env []byte, leaf bool) {
		chain := [][]byte{rootAlice, aliceBob}
		if leaf {
			chain = append(chain, env)
		} else {
			chain = append([][]byte{env}, chain...)
		}
		request := grant.Request{Domain: "example.test", Action: "read", Scope: "projects/7"}
		if _, refusal := grant.Verify(root, chain, request, grant.Time{Present: true, Now: 1799999999}); refusal != nil && (refusal.Hop < 0 || refusal.Hop >= len(chain)) {
			t.Fatalf("refusal at hop %d of %d", refusal.Hop, len(chain))
		}
		grant.Inspect(root, chain, grant.Time{})
	})
}
