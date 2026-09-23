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

// A caller cancels through the access it called through. Declared access routes
// that cancel to the destination which admitted the call, even after the tree is
// rebuilt and rebound, without another admission check.
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
			quota := duplex.AdmissionFunc(func([]string, bitwire.Message) error { checks.Add(1); return nil })
			node := func(own bitwire.Wire, policy duplex.AdmissionPolicy, children ...duplex.DeclaredChild) duplex.Declared {
				declared, err := duplex.ComposeDeclared(duplex.DeclaredValue{Own: own, Policy: policy}, children)
				if err != nil {
					t.Fatal(err)
				}
				return declared
			}
			tree := func(operation string) duplex.Declared {
				return node(duplex.RefusingOrigin{}, quota, duplex.DeclaredChild{Key: "svc", Node: node(duplex.RefusingOrigin{}, duplex.PermitAdmission{},
					duplex.DeclaredChild{Key: "run", Node: node(duplex.At(caller, []string{"svc", operation}), duplex.PermitAdmission{})})})
			}
			root := tree("held")
			for _, access := range []struct {
				name string
				wire bitwire.Wire
				path []string
			}{{"bound", root.Bind(), []string{"svc", "run"}}, {"selected", duplex.At(root.Bind(), []string{"svc"}), []string{"run"}}} {
				ctx, cancel := context.WithCancel(context.Background())
				result := make(chan error, 1)
				go func() { result <- ws.CallWire(ctx, access.wire, access.path, nil, nil) }()
				if operation := receive(t, started); operation != "held" {
					t.Fatalf("%s: call reached %s", access.name, operation)
				}
				before := checks.Load()
				// Rebuild from the parts and rebind the route to another handler.
				value, children := root.Decompose()
				if _, err := duplex.ComposeDeclared(value, children); err != nil {
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
