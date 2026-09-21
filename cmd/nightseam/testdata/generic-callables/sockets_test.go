package callables_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	binding "example.test/generated/api/go/cell-binding"
	cell "example.test/generated/api/go/cell-protocol"
	functions "example.test/generated/api/go/functions-protocol"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

type unary = functions.Function[int64, int64]
type factory = functions.Function[unary, unary]

// A reusable model knows only T. It retains the incoming lifetime along with
// the value and keeps returned values alive until the application releases it.
type memoryCell[T any] struct {
	mu       sync.Mutex
	value    T
	revision int64
	owners   []*live.Owner
}

func (c *memoryCell[T]) Replace(ctx context.Context, input cell.Put[T]) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	owner, ok := live.OwnerOf(ctx)
	if !ok {
		return 0, errors.New("replace has no active value lifetime")
	}
	c.owners = append(c.owners, owner)
	c.value = input.Value
	c.revision++
	return c.revision, nil
}

func (c *memoryCell[T]) Get(ctx context.Context) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	owner, ok := live.OwnerOf(ctx)
	if !ok {
		return c.value, errors.New("get has no active value lifetime")
	}
	c.owners = append(c.owners, owner)
	return c.value, nil
}

func (c *memoryCell[T]) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, owner := range c.owners {
		_ = owner.Release()
	}
	c.owners = nil
}

func guarded(minimum int64) unary {
	return func(_ context.Context, n int64) (int64, error) {
		if n < minimum {
			return 0, &runtime.PublicError{Code: "denied", Message: fmt.Sprintf("minimum %d", minimum)}
		}
		return n + 3, nil
	}
}

func makeFactory(seed int64) factory {
	return func(_ context.Context, callback unary) (unary, error) {
		return func(ctx context.Context, n int64) (int64, error) {
			value, err := callback(ctx, n)
			return seed + value, err
		}, nil
	}
}

func exportValue[T any](owner *live.Owner, adapter runtime.ValueAdapter[T], value T) (json.RawMessage, error) {
	ctx := live.WithOwner(context.Background(), owner)
	return live.ValueEnvironment(owner.Scope()).Export(ctx, func(ctx context.Context) (json.RawMessage, error) { return adapter.Export(ctx, value) })
}

func importValue[T any](owner *live.Owner, adapter runtime.ValueAdapter[T], raw json.RawMessage) (T, error) {
	var value T
	err := live.ValueEnvironment(owner.Scope()).Import(live.WithOwner(context.Background(), owner), func(ctx context.Context) error {
		var err error
		value, err = adapter.Import(ctx, raw)
		return err
	})
	return value, err
}

func counts(scope *live.Scope) map[string]int {
	c := scope.Counts()
	return map[string]int{"exports": c.Exports, "imports": c.Imports}
}

func waitEmpty(ctx context.Context, scope *live.Scope) error {
	for scope.Counts() != (live.Counts{}) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("counts before teardown: %+v: %w", scope.Counts(), ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	return nil
}

type socketSession struct {
	scope              *live.Scope
	cell               memoryCell[factory]
	unary              runtime.ValueAdapter[unary]
	bundle             runtime.ValueAdapter[functions.Bundle[unary]]
	complete           func(context.Context) (cell.ServerModel[factory], error)
	exporter, receiver *live.Owner
	borrowed           functions.Bundle[unary]
	effects            atomic.Int64
}

func (s *socketSession) drop() {
	s.cell.release()
	for _, owner := range []*live.Owner{s.exporter, s.receiver} {
		if owner != nil {
			_ = owner.Release()
		}
	}
	s.exporter, s.receiver = nil, nil
}

func (s *socketSession) specimen(seed int64, length int) functions.Bundle[unary] {
	makeRun := func(offset int64) unary {
		return func(_ context.Context, n int64) (int64, error) {
			if n < 0 {
				return 0, &runtime.PublicError{Code: "denied", Message: "negative"}
			}
			return seed + offset + n, nil
		}
	}
	value := functions.Bundle[unary]{Borrowed: makeRun(0)}
	for i := 0; i < length; i++ {
		value.Fresh = append(value.Fresh, makeRun(int64(i+1)))
	}
	return value
}

func (s *socketSession) controls(peer *runtime.Peer) error {
	for name, handler := range map[string]runtime.Handler{
		"test.drop":    func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { s.drop(); return nil, nil },
		"test.counts":  func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { return counts(s.scope), nil },
		"test.effects": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { return s.effects.Load(), nil },
		"test.identity_export": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) {
			if s.exporter == nil {
				s.exporter = s.scope.Owner().Child()
			}
			return exportValue(s.exporter, s.unary, func(_ context.Context, n int64) (int64, error) { s.effects.Add(1); return n + 1, nil })
		},
		"test.identity_import": func(ctx context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
			owner := s.scope.Owner().Child()
			defer owner.Release()
			value, err := importValue(owner, s.unary, raw)
			if err != nil {
				return map[string]any{"refused": true, "counts": counts(s.scope)}, nil
			}
			result, err := value(live.WithOwner(ctx, owner), 1)
			return map[string]any{"refused": false, "value": result}, err
		},
		"test.export": func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
			var input struct {
				Seed   int64
				Length int
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			if s.exporter == nil {
				s.exporter = s.scope.Owner().Child()
			}
			return exportValue(s.exporter, s.bundle, s.specimen(input.Seed, input.Length))
		},
		"test.import": func(ctx context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
			var input struct {
				Value   json.RawMessage
				Failure bool
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			if s.receiver == nil {
				s.receiver = s.scope.Owner().Child()
			}
			owner := s.receiver
			if input.Failure {
				owner = s.scope.Owner().Child()
				defer owner.Release()
			}
			value, err := importValue(owner, s.bundle, input.Value)
			if input.Failure {
				var public *runtime.PublicError
				if !errors.As(err, &public) || public.Code != live.ErrorTooManyImports {
					return nil, fmt.Errorf("expected acquisition capacity failure: %v", err)
				}
				if owner.Counts() != (live.Counts{}) || s.receiver.Counts().Imports != 3 || s.scope.Counts().Imports != 3 {
					return nil, errors.New("failed conversion changed borrowed attachments")
				}
			} else {
				if err != nil {
					return nil, err
				}
				s.borrowed = value
			}
			seen, err := s.borrowed.Borrowed(ctx, 1)
			return map[string]any{"refused": input.Failure, "seen": seen, "counts": counts(s.scope)}, err
		},
		"test.bad_export": func(ctx context.Context, _ *runtime.Peer, _ json.RawMessage) (any, error) {
			before := s.scope.Counts()
			owner := s.scope.Owner().Child()
			defer owner.Release()
			bad := functions.Bundle[unary]{Borrowed: s.borrowed.Borrowed, Fresh: []unary{guarded(0), nil}}
			if _, err := exportValue(owner, s.bundle, bad); err == nil {
				return nil, errors.New("invalid nested callable exported")
			}
			if owner.Counts() != (live.Counts{}) || s.scope.Counts() != before {
				return nil, errors.New("failed export changed prior attachments")
			}
			seen, err := s.borrowed.Borrowed(ctx, 1)
			return map[string]any{"seen": seen, "counts": counts(s.scope)}, err
		},
		"test.exercise": func(ctx context.Context, _ *runtime.Peer, _ json.RawMessage) (any, error) {
			if s.complete == nil {
				return nil, errors.New("wrong generated role")
			}
			model, err := s.complete(ctx)
			if err != nil {
				return nil, err
			}
			access, err := model(cell.Client[factory]{})
			if err != nil {
				return nil, err
			}
			owner := s.scope.Owner().Child()
			ctx = live.WithOwner(ctx, owner)
			if revision, err := access.Methods.Replace(ctx, cell.Put[factory]{Value: makeFactory(100)}); err != nil || revision != 1 {
				return nil, fmt.Errorf("replace1: %d %v", revision, err)
			}
			first, err := access.Methods.Get(ctx)
			if err != nil {
				return nil, err
			}
			if revision, err := access.Methods.Replace(ctx, cell.Put[factory]{Value: makeFactory(200)}); err != nil || revision != 2 {
				return nil, fmt.Errorf("replace2: %d %v", revision, err)
			}
			second, err := access.Methods.Get(ctx)
			if err != nil {
				return nil, err
			}
			one, err := first(ctx, guarded(0))
			if err != nil {
				return nil, err
			}
			two, err := second(ctx, guarded(10))
			if err != nil {
				return nil, err
			}
			for _, row := range []struct {
				fn          unary
				input, want int64
			}{{one, 5, 108}, {two, 15, 218}, {one, 5, 108}} {
				got, err := row.fn(ctx, row.input)
				if err != nil || got != row.want {
					return nil, fmt.Errorf("retained callable: %d %v", got, err)
				}
			}
			for _, row := range []struct {
				fn    unary
				input int64
			}{{one, -1}, {two, 5}} {
				_, err := row.fn(ctx, row.input)
				var public *runtime.PublicError
				if !errors.As(err, &public) || public.Code != "denied" {
					return nil, fmt.Errorf("distinct guard bypass: %v", err)
				}
			}
			if revision, err := access.Methods.Replace(ctx, cell.Put[factory]{Value: first}); err != nil || revision != 3 {
				return nil, fmt.Errorf("roundtrip replace: %d %v", revision, err)
			}
			roundtrip, err := access.Methods.Get(ctx)
			if err != nil {
				return nil, err
			}
			three, err := roundtrip(ctx, guarded(20))
			if err != nil {
				return nil, err
			}
			if n, err := three(ctx, 21); err != nil || n != 124 {
				return nil, fmt.Errorf("roundtrip callable: %d %v", n, err)
			}
			_, err = three(ctx, 19)
			var public *runtime.PublicError
			if !errors.As(err, &public) || public.Code != "denied" {
				return nil, fmt.Errorf("roundtrip lost guard: %v", err)
			}
			if n, err := one(ctx, 5); err != nil || n != 108 {
				return nil, fmt.Errorf("replacement invalidated returned callable: %d %v", n, err)
			}
			if owner.Counts().Exports == 0 || owner.Counts().Imports == 0 || s.scope.Owner().Counts() != (live.Counts{}) {
				return nil, errors.New("generated conversion captured root lifetime")
			}
			_ = owner.Release()
			var ignored any
			if err := peer.Call(ctx, "test.drop", struct{}{}, &ignored); err != nil {
				return nil, err
			}
			if err := waitEmpty(ctx, s.scope); err != nil {
				return nil, err
			}
			return map[string]any{"values": []int{108, 218, 108}, "released": counts(s.scope)}, nil
		},
	} {
		if err := peer.Handle(name, handler); err != nil {
			return err
		}
	}
	return nil
}

func TestGenericCallableSocketRoles(t *testing.T) {
	// Closed source alias and runtime composition describe the same nominal
	// Function application. These objects are reused by every physical scope.
	number := runtime.JSONAdapter[int64]()
	unaryAdapter := functions.AdapterFunction(number, number)
	closedAdapter := functions.AdapterIntFunction()
	composedIdentity, err := runtime.CallableIdentity(unaryAdapter.Binding)
	if err != nil {
		t.Fatal(err)
	}
	closedIdentity, err := runtime.CallableIdentity(closedAdapter.Binding)
	if err != nil {
		t.Fatal(err)
	}
	if composedIdentity != closedIdentity {
		t.Fatalf("source alias and runtime application differ: %+v %+v", closedIdentity, composedIdentity)
	}
	factoryAdapter := functions.AdapterFunction(unaryAdapter, unaryAdapter)
	bundleAdapter := functions.AdapterBundle(unaryAdapter)
	for _, role := range []string{"go-server", "typescript-server"} {
		t.Run(role, func(t *testing.T) {
			var connections atomic.Int64
			handler, err := runtime.NewHandler(runtime.ServerOptions{
				Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil }, CheckOrigin: func(*http.Request) bool { return true },
				Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
					scope, err := live.Over(peer, live.Options{MaxImports: 16, MaxExports: 128})
					if err != nil {
						return err
					}
					s := &socketSession{scope: scope, unary: closedAdapter, bundle: bundleAdapter}
					if err := s.controls(peer); err != nil {
						return err
					}
					connections.Add(1)
					if role == "go-server" {
						wire, err := binding.ToWire(func(cell.Client[factory]) (cell.Server[factory], error) {
							return cell.Server[factory]{Methods: &s.cell}, nil
						}, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)}, factoryAdapter)
						if err != nil {
							return err
						}
						detach, err := runtime.ForwardWire(peer.Wire(), wire)
						if err != nil {
							return err
						}
						go func() { <-peer.Done(); detach(); _ = wire.Close(duplex.CodeNormal, "") }()
					} else {
						var cleanup func()
						s.complete, cleanup, err = binding.PrepareFromWire(peer.Wire(), runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)}, factoryAdapter)
						if err != nil {
							return err
						}
						go func() { <-peer.Done(); cleanup() }()
					}
					return nil
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "node", "--loader", "./runtime-loader.mjs", "sockets.ts", "ws"+strings.TrimPrefix(server.URL, "http"), role)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("GEN-CALL-%s: %v\n%s", role, err, output)
			}
			if connections.Load() != 2 {
				t.Fatalf("physical scopes: %d, want two", connections.Load())
			}
		})
	}
}
