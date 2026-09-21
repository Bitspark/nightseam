package auth_test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bitspark/nightseam/auth/go"
	"github.com/Bitspark/nightseam/auth/go/grant"
)

// table is what auth-boot.json and auth-exposure.json share: keys derived
// from fixed seeds, named envelopes byte-identical to the grant table's,
// one root, and cases each naming a family.
type table struct {
	Root      jsonRoot          `json:"root"`
	Keys      map[string]key    `json:"keys"`
	Envelopes map[string]string `json:"envelopes"`
	Cases     []json.RawMessage `json:"cases"`
}

type key struct {
	Seed   string `json:"seed"`
	Pubkey string `json:"pubkey"`
}

type jsonRoot struct {
	Key    string `json:"key"`
	Domain string `json:"domain"`
}

type jsonRefusal struct {
	Code  string `json:"code"`
	Grant *struct {
		Code string `json:"code"`
		Hop  int    `json:"hop"`
	} `json:"grant"`
}

// tablesDir is conformance/tables of the checkout this module lives in.
func tablesDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.HasPrefix(string(data), "module github.com/Bitspark/nightseam\n") {
			return filepath.Join(dir, "conformance", "tables")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod of the root module above the test directory")
		}
		dir = parent
	}
}

func loadJSON(t *testing.T, name string, into any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(tablesDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatal(err)
	}
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

func (tb table) seed(t *testing.T, name string) []byte {
	t.Helper()
	k, ok := tb.Keys[name]
	if !ok {
		t.Fatalf("no key %q", name)
	}
	return unhex(t, k.Seed)
}

// pubkey resolves a key name, or 64 hex characters, to a public key.
func (tb table) pubkey(t *testing.T, s string) [32]byte {
	t.Helper()
	var out [32]byte
	if k, ok := tb.Keys[s]; ok {
		copy(out[:], unhex(t, k.Pubkey))
		return out
	}
	b := unhex(t, s)
	if len(b) != 32 {
		t.Fatalf("key %q is %d bytes", s, len(b))
	}
	copy(out[:], b)
	return out
}

func (tb table) envelope(t *testing.T, name string) []byte {
	t.Helper()
	e, ok := tb.Envelopes[name]
	if !ok {
		t.Fatalf("no envelope %q", name)
	}
	return unhex(t, e)
}

func (tb table) chain(t *testing.T, names []string) [][]byte {
	t.Helper()
	chain := make([][]byte, 0, len(names))
	for _, name := range names {
		chain = append(chain, tb.envelope(t, name))
	}
	return chain
}

func (tb table) root(t *testing.T) grant.Root {
	t.Helper()
	return grant.Root{Key: tb.pubkey(t, tb.Root.Key), Domain: tb.Root.Domain}
}

// validity reads "unbounded" or {"expires_at": n}.
func validity(t *testing.T, raw json.RawMessage) grant.Validity {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s != "unbounded" {
			t.Fatalf("validity %q", s)
		}
		return grant.Validity{}
	}
	var finite struct {
		ExpiresAt uint64 `json:"expires_at"`
	}
	if err := json.Unmarshal(raw, &finite); err != nil {
		t.Fatalf("validity %s: %v", raw, err)
	}
	return grant.Validity{Finite: true, ExpiresAt: finite.ExpiresAt}
}

// now reads a decision time: absent or null is no trusted time.
func now(raw json.RawMessage) grant.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return grant.Time{}
	}
	var n uint64
	if err := json.Unmarshal(raw, &n); err != nil {
		panic(err)
	}
	return grant.Time{Present: true, Now: n}
}

func describeRefusal(r *auth.Refusal) string {
	if r == nil {
		return "no refusal"
	}
	if r.Grant == nil {
		return string(r.Code)
	}
	return fmt.Sprintf("%s (%s at hop %d)", r.Code, r.Grant.Code, r.Grant.Hop)
}

// expectRefusal holds a refusal to the table's: the code, and the grant
// code and hop exactly when the table carries them.
func expectRefusal(t *testing.T, want jsonRefusal, got *auth.Refusal) {
	t.Helper()
	if got == nil {
		t.Fatalf("admitted, want refused %s", want.Code)
	}
	if string(got.Code) != want.Code {
		t.Fatalf("refused %s, want %s", describeRefusal(got), want.Code)
	}
	if want.Grant == nil {
		if got.Grant != nil {
			t.Fatalf("refused %s, want %s with no grant refusal", describeRefusal(got), want.Code)
		}
		return
	}
	if got.Grant == nil || string(got.Grant.Code) != want.Grant.Code || got.Grant.Hop != want.Grant.Hop {
		t.Fatalf("refused %s, want %s (%s at hop %d)", describeRefusal(got), want.Code, want.Grant.Code, want.Grant.Hop)
	}
}

// head is what every case starts with.
func head(t *testing.T, raw json.RawMessage) (family, id string) {
	t.Helper()
	var h struct {
		Family string `json:"family"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatal(err)
	}
	return h.Family, h.ID
}
