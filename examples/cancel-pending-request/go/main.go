package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	bitwire "github.com/Bitspark/bitwire/wire/go"
	"github.com/Bitspark/nightseam/duplex/go"
	ws "github.com/Bitspark/nightseam/runtime/go"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func compose(origin bitwire.Wire, children ...duplex.DeclaredChild) duplex.Declared {
	tree, err := duplex.ComposeDeclared(origin, children)
	must(err)
	return tree
}

func wait(ctx context.Context, signal <-chan struct{}) {
	select {
	case <-signal:
	case <-ctx.Done():
		panic("timed out waiting for handler: " + ctx.Err().Error())
	}
}

func main() {
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	caller, callee, err := ws.NewWirePair(ws.Options{})
	must(err)
	defer caller.Close(duplex.CodeNormal, "done")
	defer callee.Close(duplex.CodeNormal, "done")
	dispatcher, err := ws.NewDispatcher(callee)
	must(err)
	defer dispatcher.Close(duplex.CodeNormal, "done")

	started, ended := make(chan struct{}), make(chan struct{})
	_, err = ws.HandleWire(dispatcher, []string{"jobs", "original"}, func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		close(ended)
		return nil, ctx.Err()
	})
	must(err)
	_, err = ws.HandleWire(dispatcher, []string{"jobs", "replacement"}, func(context.Context, json.RawMessage) (any, error) {
		return "replacement", nil
	})
	must(err)

	root := compose(duplex.RefusingOrigin{}, duplex.DeclaredChild{
		Key: "run", Wire: duplex.At(caller, []string{"jobs", "original"}),
	})
	captured := root.Bind()
	requestContext, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- ws.CallWire(requestContext, captured, []string{"run"}, nil, nil) }()
	wait(ctx, started)

	// Replacing the assembler's description does not retarget captured access.
	origin, _ := root.Decompose()
	root = compose(origin, duplex.DeclaredChild{
		Key: "run", Wire: duplex.At(caller, []string{"jobs", "replacement"}),
	})
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			panic(fmt.Sprintf("wanted caller cancellation, got %v", err))
		}
	case <-ctx.Done():
		panic("caller did not finish")
	}
	// Caller completion alone is insufficient: the original body must see cancellation.
	wait(ctx, ended)
	fmt.Println("original request: cancelled")

	var fresh string
	must(ws.CallWire(ctx, root.Bind(), []string{"run"}, nil, &fresh))
	if fresh != "replacement" {
		panic("fresh call did not reach replacement")
	}
	fmt.Println("new request:", fresh)
}
