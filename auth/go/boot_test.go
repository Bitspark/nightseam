package auth_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"sort"
	"testing"

	"github.com/Bitspark/archon/sdk/go/login"
	"github.com/Bitspark/nightseam/auth/go"
	"github.com/Bitspark/nightseam/auth/go/grant"
)

// memoryStore is the consumer's store as a test supplies it: one map, a
// versioned compare-and-set, a sweep by the record's own end.
type memoryStore struct {
	records map[string]auth.Record
}

func newMemoryStore() *memoryStore {
	return &memoryStore{records: make(map[string]auth.Record)}
}

func (s *memoryStore) Get(id []byte) (auth.Record, bool) {
	r, ok := s.records[hex.EncodeToString(id)]
	return r, ok
}

func (s *memoryStore) Put(r auth.Record, expectVersion int) bool {
	key := hex.EncodeToString(r.ID)
	current, ok := s.records[key]
	if (!ok && expectVersion != 0) || (ok && current.Version != expectVersion) {
		return false
	}
	s.records[key] = r
	return true
}

func (s *memoryStore) Sweep(now uint64) [][]byte {
	var dropped [][]byte
	keys := make([]string, 0, len(s.records))
	for key := range s.records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if r := s.records[key]; r.Gone(now) {
			dropped = append(dropped, r.ID)
			delete(s.records, key)
		}
	}
	return dropped
}

func (s *memoryStore) Count() int {
	return len(s.records)
}

// TestBootTable reproduces every case of auth-boot.json: the count of
// cases run is the count of cases in the file, and every family's output
// is held as the table states it — audiences, bytes, verdicts, and the
// scripts step by step with the record's state after each.
func TestBootTable(t *testing.T) {
	var tb table
	loadJSON(t, "auth-boot.json", &tb)
	const want = 52
	if len(tb.Cases) != want {
		t.Fatalf("%d cases in the table, the packet states %d", len(tb.Cases), want)
	}
	ran := 0
	for _, raw := range tb.Cases {
		family, id := head(t, raw)
		if t.Run(id, func(t *testing.T) {
			switch family {
			case "boot_audience":
				runAudience(t, raw)
			case "auth_binding":
				runBinding(t, raw)
			case "auth_prove":
				tb.runProve(t, raw)
			case "auth_connect":
				tb.runConnect(t, raw)
			case "auth_call":
				tb.runCall(t, raw)
			case "boot_login":
				tb.runLogin(t, raw)
			default:
				t.Fatalf("unknown family %q", family)
			}
		}) {
			ran++
		}
	}
	if ran != want {
		t.Fatalf("%d of %d cases reproduced", ran, want)
	}
}

func runAudience(t *testing.T, raw json.RawMessage) {
	var c struct {
		URL      string `json:"url"`
		Audience string `json:"audience"`
		Refused  bool   `json:"refused"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	got, err := auth.Audience(c.URL)
	if c.Refused {
		if err == nil {
			t.Fatalf("derived %q, want refused", got)
		}
		return
	}
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if got != c.Audience {
		t.Fatalf("derived %q, want %q", got, c.Audience)
	}
	// The audience is a fixed point of its own grammar, and Archon's
	// derivation of the same URL with a login tail agrees.
	if again, err := auth.Audience(got); err != nil || again != got {
		t.Fatalf("the audience %q is not a fixed point: %q, %v", got, again, err)
	}
	theirs, _, err := login.DeriveAudience(c.URL + "/login/00")
	if err != nil || theirs != got {
		t.Fatalf("Archon derives %q (%v), this package %q", theirs, err, got)
	}
}

func runBinding(t *testing.T, raw json.RawMessage) {
	var c struct {
		Audience string `json:"audience"`
		Binding  string `json:"binding"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	got, err := auth.ConnectionBinding(c.Audience)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, unhex(t, c.Binding)) {
		t.Fatalf("binding %x, want %s", got, c.Binding)
	}
}

func (tb table) runProve(t *testing.T, raw json.RawMessage) {
	var c struct {
		Key      string `json:"key"`
		Audience string `json:"audience"`
		Nonce    string `json:"nonce"`
		Proof    string `json:"proof"`
		Verifies bool   `json:"verifies"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	pubkey := tb.pubkey(t, c.Key)
	nonce, proof := unhex(t, c.Nonce), unhex(t, c.Proof)
	if got := auth.Verify(pubkey[:], c.Audience, nonce, proof); got != c.Verifies {
		t.Fatalf("verifies %v, want %v", got, c.Verifies)
	}
	if c.Verifies {
		made, err := auth.Prove(tb.seed(t, c.Key), c.Audience, nonce)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(made, proof) {
			t.Fatalf("proved %x, want %s", made, c.Proof)
		}
	}
}

func (tb table) runConnect(t *testing.T, raw json.RawMessage) {
	var c struct {
		Audience string `json:"audience"`
		Steps    []struct {
			Op      string          `json:"op"`
			Nonce   string          `json:"nonce"`
			Subject string          `json:"subject"`
			Proof   string          `json:"proof"`
			Chain   []string        `json:"chain"`
			Now     json.RawMessage `json:"now"`
			Result  json.RawMessage `json:"result"`
			Refused *jsonRefusal    `json:"refused"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	conn, err := auth.NewConnection(c.Audience)
	if err != nil {
		t.Fatal(err)
	}
	root := tb.root(t)
	for i, step := range c.Steps {
		switch step.Op {
		case "challenge":
			nonce, refusal := conn.Challenge(unhex(t, step.Nonce))
			if step.Refused != nil {
				expectRefusal(t, *step.Refused, refusal)
				continue
			}
			if refusal != nil {
				t.Fatalf("step %d: refused %s", i, describeRefusal(refusal))
			}
			if string(step.Result) != `"challenged"` || !bytes.Equal(nonce, unhex(t, step.Nonce)) {
				t.Fatalf("step %d: answered nonce %x for result %s", i, nonce, step.Result)
			}
			if conn.Context() != nil {
				t.Fatalf("step %d: a challenge made a context", i)
			}
		case "prove":
			ctx, refusal := conn.Prove(root, tb.pubkey(t, step.Subject), unhex(t, step.Proof), tb.chain(t, step.Chain), now(step.Now))
			if step.Refused != nil {
				expectRefusal(t, *step.Refused, refusal)
				if ctx != nil {
					t.Fatalf("step %d: a refused prove made a context", i)
				}
				continue
			}
			if refusal != nil {
				t.Fatalf("step %d: refused %s", i, describeRefusal(refusal))
			}
			var want struct {
				Since    uint64          `json:"since"`
				Subject  string          `json:"subject"`
				Validity json.RawMessage `json:"validity"`
			}
			if err := json.Unmarshal(step.Result, &want); err != nil {
				t.Fatal(err)
			}
			if ctx == nil || ctx != conn.Context() {
				t.Fatalf("step %d: the context answered is not the connection's", i)
			}
			if ctx.Since != want.Since || ctx.Subject != tb.pubkey(t, want.Subject) || ctx.Validity != validity(t, want.Validity) {
				t.Fatalf("step %d: context since %d subject %x validity %+v, want since %d subject %s validity %s", i, ctx.Since, ctx.Subject, ctx.Validity, want.Since, want.Subject, want.Validity)
			}
			chain := tb.chain(t, step.Chain)
			if len(ctx.Chain) != len(chain) {
				t.Fatalf("step %d: the context keeps %d envelopes of %d", i, len(ctx.Chain), len(chain))
			}
			for j := range chain {
				if !bytes.Equal(ctx.Chain[j], chain[j]) {
					t.Fatalf("step %d: envelope %d of the context differs", i, j)
				}
			}
		default:
			t.Fatalf("step %d: unknown op %q", i, step.Op)
		}
	}
}

func (tb table) runCall(t *testing.T, raw json.RawMessage) {
	var c struct {
		Subject string          `json:"subject"`
		Chain   []string        `json:"chain"`
		Request grant.Request   `json:"request"`
		Now     json.RawMessage `json:"now"`
		Allowed *struct {
			Subject  string          `json:"subject"`
			Depth    uint8           `json:"depth"`
			Validity json.RawMessage `json:"validity"`
			Hops     int             `json:"hops"`
		} `json:"allowed"`
		Refused *jsonRefusal `json:"refused"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	var ctx *auth.Context
	if c.Subject != "" {
		ctx = &auth.Context{Subject: tb.pubkey(t, c.Subject), Chain: tb.chain(t, c.Chain)}
	}
	verified, refusal := auth.Call(tb.root(t), ctx, c.Request, now(c.Now))
	if c.Refused != nil {
		expectRefusal(t, *c.Refused, refusal)
		return
	}
	if refusal != nil {
		t.Fatalf("refused %s", describeRefusal(refusal))
	}
	if verified.Subject != tb.pubkey(t, c.Allowed.Subject) || verified.Depth != c.Allowed.Depth || verified.Validity != validity(t, c.Allowed.Validity) || verified.Hops != c.Allowed.Hops {
		t.Fatalf("allowed %+v, want %+v", verified, c.Allowed)
	}
}

// loginStep is one step of a boot_login transcript; which fields a step
// carries depends on its op.
type loginStep struct {
	Op        string          `json:"op"`
	Now       uint64          `json:"now"`
	ID        string          `json:"id"`
	Nonce     string          `json:"nonce"`
	Browser   string          `json:"browser"`
	Scope     []string        `json:"scope"`
	ValidFor  uint32          `json:"valid_for"`
	Principal string          `json:"principal"`
	Proof     string          `json:"proof"`
	Authority []string        `json:"authority"`
	Version   int             `json:"version"`
	Result    json.RawMessage `json:"result"`
	Refused   json.RawMessage `json:"refused"`
	State     string          `json:"state"`
}

// refusedLogin reads a refusal that is a bare code or a code with a grant
// refusal.
func refusedLogin(t *testing.T, raw json.RawMessage) (code string, refusal *jsonRefusal) {
	t.Helper()
	if err := json.Unmarshal(raw, &code); err == nil {
		return code, nil
	}
	var r jsonRefusal
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("refused %s: %v", raw, err)
	}
	return r.Code, &r
}

func (tb table) runLogin(t *testing.T, raw json.RawMessage) {
	var c struct {
		Audience string      `json:"audience"`
		Steps    []loginStep `json:"steps"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	store := newMemoryStore()
	service := &auth.Service{Root: tb.root(t), Audience: c.Audience, Store: store}
	for i, step := range c.Steps {
		var code auth.LoginCode
		var refusal *grant.Refusal
		switch step.Op {
		case "begin":
			var record auth.Record
			record, code = service.Begin(step.Now, unhex(t, step.ID), unhex(t, step.Nonce), tb.pubkeyBytes(t, step.Browser), step.Scope, step.ValidFor)
			if code == "" {
				var want struct {
					ExpiresAt uint64 `json:"expires_at"`
					Version   int    `json:"version"`
				}
				if err := json.Unmarshal(step.Result, &want); err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				if record.ExpiresAt != want.ExpiresAt || record.Version != want.Version || record.State != auth.Pending {
					t.Fatalf("step %d: began %+v, want expires_at %d version %d pending", i, record, want.ExpiresAt, want.Version)
				}
			}
		case "read":
			var record auth.Record
			record, code = service.Read(step.Now, unhex(t, step.ID))
			if code == "" {
				var want struct {
					Browser   string   `json:"browser"`
					ExpiresAt uint64   `json:"expires_at"`
					Nonce     string   `json:"nonce"`
					Scope     []string `json:"scope"`
					ValidFor  uint32   `json:"valid_for"`
				}
				if err := json.Unmarshal(step.Result, &want); err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				if record.Browser != tb.pubkey(t, want.Browser) || record.ExpiresAt != want.ExpiresAt || !bytes.Equal(record.Nonce, unhex(t, want.Nonce)) || !sameStrings(record.Scope, want.Scope) || record.ValidFor != want.ValidFor {
					t.Fatalf("step %d: read %+v, want %+v", i, record, want)
				}
			}
		case "answer":
			code, refusal = service.Answer(step.Now, unhex(t, step.ID), tb.pubkey(t, step.Principal), unhex(t, step.Proof), tb.chain(t, step.Authority), step.Version)
			if code == "" && string(step.Result) != `"answered"` {
				t.Fatalf("step %d: answered, want %s", i, step.Result)
			}
		case "collect":
			var answer auth.Answer
			answer, code = service.Collect(step.Now, unhex(t, step.ID), unhex(t, step.Proof))
			if code == "" {
				var want struct {
					Authority  []string `json:"authority"`
					Possession string   `json:"possession"`
					Principal  string   `json:"principal"`
				}
				if err := json.Unmarshal(step.Result, &want); err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				if answer.Principal != tb.pubkey(t, want.Principal) || !bytes.Equal(answer.Possession, unhex(t, want.Possession)) || !sameChains(answer.Authority, tb.chain(t, want.Authority)) {
					t.Fatalf("step %d: collected another answer", i)
				}
			}
		case "sweep":
			dropped := service.Sweep(step.Now)
			var want []string
			if err := json.Unmarshal(step.Result, &want); err != nil {
				t.Fatalf("step %d: %v", i, err)
			}
			got := make([]string, 0, len(dropped))
			for _, id := range dropped {
				got = append(got, hex.EncodeToString(id))
			}
			if !sameStrings(got, want) {
				t.Fatalf("step %d: swept %v, want %v", i, got, want)
			}
			continue
		default:
			t.Fatalf("step %d: unknown op %q", i, step.Op)
		}
		if len(step.Refused) > 0 {
			wantCode, wantRefusal := refusedLogin(t, step.Refused)
			if string(code) != wantCode {
				t.Fatalf("step %d: %s answered %q, want refused %q", i, step.Op, code, wantCode)
			}
			if wantRefusal == nil || wantRefusal.Grant == nil {
				if refusal != nil {
					t.Fatalf("step %d: refused %q with grant refusal %s at hop %d, want none", i, code, refusal.Code, refusal.Hop)
				}
			} else if refusal == nil || string(refusal.Code) != wantRefusal.Grant.Code || refusal.Hop != wantRefusal.Grant.Hop {
				t.Fatalf("step %d: refused %q with grant refusal %v, want %s at hop %d", i, code, refusal, wantRefusal.Grant.Code, wantRefusal.Grant.Hop)
			}
		} else if code != "" {
			t.Fatalf("step %d: %s refused %q (%v), want %s", i, step.Op, code, refusal, step.Result)
		}
		if step.State != "" {
			record, ok := store.Get(unhex(t, step.ID))
			if !ok || string(record.State) != step.State {
				t.Fatalf("step %d: the record is %q (%v), want %s", i, record.State, ok, step.State)
			}
		}
	}
}

func (tb table) pubkeyBytes(t *testing.T, name string) []byte {
	t.Helper()
	k := tb.pubkey(t, name)
	return k[:]
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameChains(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
