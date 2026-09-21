package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/duplex/go"
	runtime "github.com/Bitspark/nightseam/runtime/go"
)

const identityDigest = "4433469c3fb5e66b667a7b4463cb878ab59d214bb1f9163e92bc2005b9987cc3"
const otherIdentityDigest = "c4dbce5dafcb23c6863de70876927aee504ca5323397908f8abacebc089322b1"

func identityPair(t *testing.T, handlers map[string]runtime.Handler) *runtime.Peer {
	t.Helper()
	a, b := duplex.Pipe(1 << 20)
	server, err := runtime.NewPeer(t.Context(), b, runtime.ServerRole, runtime.Options{Handlers: handlers})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	client, err := runtime.NewPeer(t.Context(), a, runtime.ClientRole, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func identityCode(t *testing.T, err error, code string) {
	t.Helper()
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestIdentityExchangePrecedesModelEffects(t *testing.T) {
	for _, row := range []struct {
		name, path, digest string
		mismatch           bool
	}{
		{"same", "世界/service", identityDigest, false},
		{"different digest", "世界/service", otherIdentityDigest, true},
		{"different path", "other", identityDigest, true},
		{"remote digest absent", "世界/service", "", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			handler, err := runtime.IdentityHandler(runtime.DeclarationIdentity{Path: row.path, Digest: row.digest})
			if err != nil {
				t.Fatal(err)
			}
			var effects atomic.Int32
			peer := identityPair(t, map[string]runtime.Handler{
				runtime.IdentityMethod: handler,
				"model":                func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { effects.Add(1); return nil, nil },
			})
			err = runtime.CheckIdentity(t.Context(), peer.Call, runtime.DeclarationIdentity{Path: "世界/service", Digest: identityDigest})
			if effects.Load() != 0 {
				t.Fatal("identity exchange dispatched the model")
			}
			if row.mismatch {
				identityCode(t, err, "contract_mismatch")
				if !strings.Contains(err.Error(), row.path) {
					t.Fatal("mismatch did not name the expected serving path")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			// A refusal does not close the carrier; the caller controls whether
			// to expose its model after this check.
			if err := peer.Call(t.Context(), "model", nil, nil); err != nil {
				t.Fatal(err)
			}
			if effects.Load() != 1 {
				t.Fatal("model did not remain usable")
			}
		})
	}
	handler, err := runtime.IdentityHandler(runtime.DeclarationIdentity{Path: "same", Digest: identityDigest})
	if err != nil {
		t.Fatal(err)
	}
	peer := identityPair(t, map[string]runtime.Handler{runtime.IdentityMethod: handler})
	if err := runtime.CheckIdentity(t.Context(), peer.Call, runtime.DeclarationIdentity{Path: "same"}); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityExchangeOnlyAcceptsMethodNotFound(t *testing.T) {
	peer := identityPair(t, nil)
	if err := runtime.CheckIdentity(t.Context(), peer.Call, runtime.DeclarationIdentity{Path: "same", Digest: identityDigest}); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"contract_invalid", "busy", "internal", "contract_mismatch"} {
		peer := identityPair(t, map[string]runtime.Handler{runtime.IdentityMethod: func(context.Context, *runtime.Peer, json.RawMessage) (any, error) {
			return nil, &runtime.PublicError{Code: code, Message: "refused"}
		}})
		identityCode(t, runtime.CheckIdentity(t.Context(), peer.Call, runtime.DeclarationIdentity{Path: "same"}), code)
	}
	closed := identityPair(t, nil)
	closed.Close()
	if err := runtime.CheckIdentity(t.Context(), closed.Call, runtime.DeclarationIdentity{Path: "same"}); err == nil || !errors.Is(err, closed.Err()) {
		t.Fatalf("closed carrier refusal was not preserved: %v", err)
	}
}

func TestIdentityExchangeRejectsMalformedPayloadsAndAnswers(t *testing.T) {
	malformed := []string{`null`, `[]`, `{}`, `{"path":""}`, `{"path":1}`, `{"path":"same","extra":true}`, `{"path":"same","digest":""}`, `{"path":"same","digest":null}`, `{"path":"same","digest":1}`, `{"path":"same","digest":"short"}`, `{"path":"same","digest":"` + strings.Repeat("A", 64) + `"}`}
	handler, err := runtime.IdentityHandler(runtime.DeclarationIdentity{Path: "same", Digest: identityDigest})
	if err != nil {
		t.Fatal(err)
	}
	peer := identityPair(t, map[string]runtime.Handler{runtime.IdentityMethod: handler})
	for _, raw := range malformed {
		identityCode(t, peer.Call(t.Context(), runtime.IdentityMethod, json.RawMessage(raw), nil), "contract_invalid")
		remote := identityPair(t, map[string]runtime.Handler{runtime.IdentityMethod: func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { return json.RawMessage(raw), nil }})
		identityCode(t, runtime.CheckIdentity(t.Context(), remote.Call, runtime.DeclarationIdentity{Path: "same"}), "contract_invalid")
	}
	for _, raw := range []string{`{"path":"\uD800"}`, `{"path":"same","digest":"` + identityDigest + `\n"}`} {
		_, err := handler(t.Context(), nil, json.RawMessage(raw))
		identityCode(t, err, "contract_invalid")
	}
	for _, local := range []runtime.DeclarationIdentity{{}, {Path: string([]byte{0xff})}, {Path: "same", Digest: "short"}} {
		_, err := runtime.IdentityHandler(local)
		identityCode(t, err, "contract_invalid")
		called := false
		err = runtime.CheckIdentity(t.Context(), func(context.Context, string, any, any) error { called = true; return nil }, local)
		identityCode(t, err, "contract_invalid")
		if called {
			t.Fatal("invalid local identity reached the carrier")
		}
	}
}

func TestIdentityExchangeValidatesReturnedIdentity(t *testing.T) {
	peer := identityPair(t, map[string]runtime.Handler{runtime.IdentityMethod: func(context.Context, *runtime.Peer, json.RawMessage) (any, error) {
		return runtime.DeclarationIdentity{Path: "same", Digest: otherIdentityDigest}, nil
	}})
	err := runtime.CheckIdentity(t.Context(), peer.Call, runtime.DeclarationIdentity{Path: "same", Digest: identityDigest})
	identityCode(t, err, "contract_mismatch")
	if !strings.Contains(err.Error(), "same") {
		t.Fatal("mismatch did not name the local expected path")
	}
}

func TestIdentityExchangeDeadline(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	peer := identityPair(t, map[string]runtime.Handler{runtime.IdentityMethod: func(ctx context.Context, _ *runtime.Peer, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := runtime.CheckIdentity(ctx, peer.Call, runtime.DeclarationIdentity{Path: "same"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
	receive(t, started)
	receive(t, cancelled)
	for _, deadline := range []time.Duration{0, time.Second} {
		ctx := context.Background()
		cancel := func() {}
		if deadline != 0 {
			ctx, cancel = context.WithTimeout(ctx, deadline)
		}
		err := runtime.CheckIdentity(ctx, func(ctx context.Context, method string, params, result any) error {
			until, ok := ctx.Deadline()
			if !ok {
				t.Fatal("unbounded identity call")
			}
			want := deadline
			if want == 0 {
				want = 30 * time.Second
			}
			if remaining := time.Until(until); remaining > want || remaining < want-time.Second {
				t.Fatalf("wrong deadline: %s", remaining)
			}
			if method != runtime.IdentityMethod {
				t.Fatal(method)
			}
			raw, _ := json.Marshal(params)
			if strings.Contains(string(raw), "digest") {
				t.Fatal("absent digest was emitted")
			}
			return &runtime.PublicError{Code: "method_not_found"}
		}, runtime.DeclarationIdentity{Path: "same"})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
}
