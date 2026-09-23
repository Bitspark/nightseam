package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	bitwire "github.com/Bitspark/bitwire/wire/go"

	"github.com/Bitspark/nightseam/duplex/go"
	ws "github.com/Bitspark/nightseam/runtime/go"
)

// A caller cancels through the same captured access and path. Plain composition
// delegates it unchanged; the destination profile owns the admitted invocation.
type declaredCancellationGuard struct {
	inner  bitwire.Wire
	checks *atomic.Int32
}

func (g *declaredCancellationGuard) Send(path []string, message bitwire.Message) error {
	if message.Frame.Kind == bitwire.ProfileRequest || message.Frame.Kind == bitwire.ProfileEvent {
		g.checks.Add(1)
	}
	return g.inner.Send(path, message)
}

func TestDeclaredAccessCarriesItsCallersCancellation(t *testing.T) {
	carriers := map[string]func(t *testing.T) (bitwire.Endpoint, bitwire.Endpoint){
		"local pair": func(t *testing.T) (bitwire.Endpoint, bitwire.Endpoint) {
			left, right, err := ws.NewWirePair(ws.Options{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = left.Close(duplex.CodeNormal, "done"); _ = right.Close(duplex.CodeNormal, "done") })
			return left, right
		},
		"peers": func(t *testing.T) (bitwire.Endpoint, bitwire.Endpoint) {
			client, server := newPair(t, ws.Options{}, ws.Options{})
			return client.Wire(), server.Wire()
		},
	}
	for name, carrier := range carriers {
		t.Run(name, func(t *testing.T) {
			caller, callee := carrier(t)
			started := make(chan string, 2)
			cancelled := make(chan string, 2)
			binding := testBinding(t, callee)
			for _, operation := range []string{"held", "replacement"} {
				if _, err := ws.HandleWire(binding, []string{"svc", operation}, func(ctx context.Context, _ json.RawMessage) (any, error) {
					started <- operation
					<-ctx.Done()
					cancelled <- operation
					return nil, ctx.Err()
				}); err != nil {
					t.Fatal(err)
				}
			}
			var checks atomic.Int32
			node := func(origin bitwire.Wire, children ...duplex.DeclaredChild) duplex.Declared {
				declared, err := duplex.ComposeDeclared(origin, children)
				if err != nil {
					t.Fatal(err)
				}
				return declared
			}
			tree := func(operation string) duplex.Declared {
				branch := node(duplex.RefusingOrigin{}, duplex.DeclaredChild{Key: "run", Wire: duplex.At(caller, []string{"svc", operation})})
				// A consumer guard is complete child access; it owns its checks and
				// passes controls through to the runtime's captured invocation.
				guard := &declaredCancellationGuard{inner: branch.Bind(), checks: &checks}
				return node(duplex.RefusingOrigin{}, duplex.DeclaredChild{Key: "svc", Wire: guard})
			}
			root := tree("held")
			origin, children := root.Decompose()
			rebuilt := node(origin, children...)
			for _, access := range []struct {
				name string
				wire bitwire.Wire
				path []string
			}{{"bound", root.Bind(), []string{"svc", "run"}}, {"selected", duplex.At(root.Bind(), []string{"svc"}), []string{"run"}}, {"reconstructed", rebuilt.Bind(), []string{"svc", "run"}}} {
				ctx, cancel := context.WithCancel(context.Background())
				result := make(chan error, 1)
				go func() { result <- ws.CallWire(ctx, access.wire, access.path, nil, nil) }()
				if operation := receive(t, started); operation != "held" {
					t.Fatalf("%s: call reached %s", access.name, operation)
				}
				before := checks.Load()
				// Rebuild from the parts and rebind the route to another handler.
				origin, children := root.Decompose()
				if _, err := duplex.ComposeDeclared(origin, children); err != nil {
					t.Fatal(err)
				}
				root = tree("replacement")
				cancel()
				if err := receive(t, result); !errors.Is(err, context.Canceled) {
					t.Fatalf("%s: call = %v", access.name, err)
				}
				if operation := receive(t, cancelled); operation != "held" {
					t.Fatalf("%s: cancellation reached %s", access.name, operation)
				}
				if checks.Load() != before {
					t.Fatalf("%s: the cancel entered admission again", access.name)
				}
				root = tree("held")
			}
		})
	}
}
