package composition_test

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

	binding "example.com/probe/api/go/compose-cell-binding"
	cell "example.com/probe/api/go/compose-cell-protocol"
	functions "example.com/probe/api/go/functions-protocol"
	holder "example.com/probe/api/go/holder-protocol"
	numbers "example.com/probe/api/go/numbers-protocol"
	texts "example.com/probe/api/go/texts-protocol"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

// The same state implementation serves every recipe. It never names a live
// type or converter, and retains each supplying call's lifetime explicitly.
type memoryCell[T any] struct {
	mu       sync.Mutex
	value    T
	revision int64
	owners   []*live.Owner
}

func (m *memoryCell[T]) retain(ctx context.Context) error {
	owner, ok := live.OwnerOf(ctx)
	if !ok {
		return errors.New("generated call has no value lifetime")
	}
	m.owners = append(m.owners, owner)
	return nil
}

func (m *memoryCell[T]) Put(ctx context.Context, input cell.Put[T]) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.retain(ctx); err != nil {
		return 0, err
	}
	m.value = input.Value
	m.revision++
	return m.revision, nil
}

func (m *memoryCell[T]) Get(ctx context.Context) (T, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.value, m.retain(ctx)
}

func (m *memoryCell[T]) release() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, owner := range m.owners {
		_ = owner.Release()
	}
	m.owners = nil
}

func serve[T any](t *testing.T, name string, adapter runtime.ValueAdapter[T]) {
	t.Helper()
	var connections atomic.Int64
	handler, err := runtime.NewHandler(runtime.ServerOptions{
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
		Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
			connections.Add(1)
			scope, err := live.Over(peer, live.Options{})
			if err != nil {
				return err
			}
			model := &memoryCell[T]{}
			wire, err := binding.ToWire(func(cell.Client[T]) (cell.Server[T], error) { return cell.Server[T]{Methods: model}, nil }, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)}, adapter)
			if err != nil {
				return err
			}
			detach, err := runtime.ForwardWire(peer.Wire(), wire)
			if err != nil {
				return err
			}
			go func() { <-peer.Done(); model.release(); detach(); _ = wire.Close(duplex.CodeNormal, "") }()
			return peer.Handle("smoke.drop", func(ctx context.Context, _ *runtime.Peer, _ json.RawMessage) (any, error) {
				if scope.Owner().Counts() != (live.Counts{}) {
					return nil, errors.New("generic recipe captured root lifetime")
				}
				model.release()
				deadline := time.Now().Add(5 * time.Second)
				for scope.Counts() != (live.Counts{}) {
					if time.Now().After(deadline) {
						return nil, fmt.Errorf("bindings remained before teardown: %+v", scope.Counts())
					}
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(time.Millisecond):
					}
				}
				return map[string]int{"exports": 0, "imports": 0}, nil
			})
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "--experimental-strip-types", "--disable-warning=ExperimentalWarning", "main.ts", "ws"+strings.TrimPrefix(server.URL, "http"), name)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("packed generic %s: %v\n%s", name, err, output)
	} else {
		t.Log(strings.TrimSpace(string(output)))
	}
	if connections.Load() != 1 {
		t.Fatalf("physical connections: %d, want one", connections.Load())
	}
}

func TestPackedGenericComposition(t *testing.T) {
	// Recipe construction precedes connection construction; providers are
	// supplied without editing or regenerating the Cell or Holder artifact.
	number := runtime.JSONAdapter[int64]()
	t.Run("function", func(t *testing.T) { serve(t, "function", functions.AdapterFunction(number, number)) })
	t.Run("numbers", func(t *testing.T) {
		serve(t, "numbers", holder.AdapterBatch(numbers.AdapterJob(), numbers.AdapterProgress()))
	})
	t.Run("texts", func(t *testing.T) {
		serve(t, "texts", holder.AdapterBatch(texts.AdapterJob(), texts.AdapterProgress()))
	})
}
