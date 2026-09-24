// This is application code using Nightseam's public Wire RPC adapters.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
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

func require(ok bool, message string) {
	if !ok {
		panic(message)
	}
}

func compose(origin bitwire.Wire, children ...duplex.DeclaredChild) duplex.Declared {
	tree, err := duplex.ComposeDeclared(origin, children)
	must(err)
	return tree
}

func call[T any](ctx context.Context, access bitwire.Wire, path []string, params any) T {
	var result T
	must(ws.CallWire(ctx, access, path, params, &result))
	return result
}

// The application owns state and endpoints; composition only borrows access.
func bookshop() (origin, cart, recommendations bitwire.Wire, close func()) {
	caller, callee, err := ws.NewWirePair(ws.Options{})
	must(err)
	dispatcher, err := ws.NewDispatcher(callee)
	must(err)
	handle := func(path []string, handler ws.WireHandler) {
		_, err := ws.HandleWire(dispatcher, path, handler)
		must(err)
	}
	items := []string{"book", "pen"}
	var mu sync.Mutex
	handle([]string{"storeInfo"}, func(context.Context, json.RawMessage) (any, error) {
		return "Bookshop", nil
	})
	handle([]string{"cart", "list"}, func(context.Context, json.RawMessage) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), items...), nil
	})
	handle([]string{"cart", "add"}, func(_ context.Context, params json.RawMessage) (any, error) {
		var item string
		if err := json.Unmarshal(params, &item); err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		items = append(items, item)
		return nil, nil
	})
	handle([]string{"recommendations"}, func(context.Context, json.RawMessage) (any, error) {
		return []string{"pencil"}, nil
	})
	return duplex.At(caller, []string{"storeInfo"}), duplex.At(caller, []string{"cart"}),
		duplex.At(caller, []string{"recommendations"}), func() {
			must(dispatcher.Close(duplex.CodeNormal, "done"))
			must(caller.Close(duplex.CodeNormal, "done"))
			must(callee.Close(duplex.CodeNormal, "done"))
		}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	origin, cart, recommendations, close := bookshop()
	defer close()
	shop := compose(origin, duplex.DeclaredChild{Key: "cart", Wire: cart})
	before := call[[]string](ctx, shop.Bind(), []string{"cart", "list"}, nil)
	require(reflect.DeepEqual(before, []string{"book", "pen"}), "unexpected initial state")
	// Retain the complete cart access, including whatever state or guard it holds.
	parent, children := shop.Decompose()
	sameCart := len(children) == 1 && children[0].Wire == cart
	require(sameCart && parent == origin, "construction lost capability identity")
	expanded := compose(parent, append(children, duplex.DeclaredChild{Key: "recommendations", Wire: recommendations})...)
	call[any](ctx, expanded.Bind(), []string{"cart", "add"}, "notebook")
	// The old access still reaches the same cart.
	after := call[[]string](ctx, shop.Bind(), []string{"cart", "list"}, nil)
	require(reflect.DeepEqual(after, []string{"book", "pen", "notebook"}), "rebuild reset cart state")
	require(reflect.DeepEqual(after, call[[]string](ctx, expanded.Bind(), []string{"cart", "list"}, nil)), "new access differs")
	name := call[string](ctx, expanded.Bind(), nil, nil)
	suggestions := call[[]string](ctx, expanded.Bind(), []string{"recommendations"}, nil)
	require(name == "Bookshop", "rebuild lost parent behavior")
	require(reflect.DeepEqual(suggestions, []string{"pencil"}), "new child is missing")
	fmt.Println("before:", strings.Join(before, ", "))
	fmt.Println("same cart capability:", sameCart)
	fmt.Println("shop:", name)
	fmt.Println("after:", strings.Join(after, ", "))
	fmt.Println("recommendations:", strings.Join(suggestions, ", "))
}
