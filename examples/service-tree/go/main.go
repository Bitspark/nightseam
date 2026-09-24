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
	origin, cart, _, close := bookshop()
	defer close()
	shop := compose(origin, duplex.DeclaredChild{Key: "cart", Wire: cart}).Bind()
	name := call[string](ctx, shop, nil, nil)
	items := call[[]string](ctx, shop, []string{"cart", "list"}, nil)
	require(name == "Bookshop", "parent behavior changed")
	require(reflect.DeepEqual(items, []string{"book", "pen"}), "unexpected cart")
	fmt.Println("shop:", name)
	fmt.Println("cart:", strings.Join(items, ", "))
}
