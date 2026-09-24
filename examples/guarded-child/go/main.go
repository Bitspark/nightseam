// This is application code using Nightseam's public Wire RPC adapters.
package main

import (
	"context"
	"encoding/json"
	"errors"
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
	guard := &budgetedCart{inner: cart, remaining: 2}
	shop := compose(origin, duplex.DeclaredChild{Key: "cart", Wire: guard})
	call[any](ctx, shop.Bind(), []string{"cart", "add"}, "notebook")
	require(guard.left() == 1, "first add did not consume one admission")
	fmt.Println("remaining after first add:", guard.left())
	parent, children := shop.Decompose()
	require(children[0].Wire == guard, "guard was replaced")
	expanded := compose(parent, append(children, duplex.DeclaredChild{Key: "recommendations", Wire: recommendations})...)
	call[any](ctx, expanded.Bind(), []string{"cart", "add"}, "pencil")
	require(guard.left() == 0, "rebuild reset the budget")
	fmt.Println("remaining after rebuild and second add:", guard.left())
	err := ws.CallWire(ctx, expanded.Bind(), []string{"cart", "add"}, "eraser", nil)
	require(errors.Is(err, errBudget), "third add was not refused by the retained guard")
	fmt.Println("third add: refused")
	items := call[[]string](ctx, shop.Bind(), []string{"cart", "list"}, nil)
	require(reflect.DeepEqual(items, []string{"book", "pen", "notebook", "pencil"}), "refused add changed the cart")
	fmt.Println("cart:", strings.Join(items, ", "))
}

var errBudget = errors.New("cart admission budget exhausted")

// This policy counts admitted add requests, not completed effects.
type budgetedCart struct {
	inner     bitwire.Wire
	mu        sync.Mutex
	remaining int
}

func (g *budgetedCart) Send(path []string, message bitwire.Message) error {
	charged := message.Frame.Kind == bitwire.ProfileRequest && len(path) == 1 && path[0] == "add"
	if !charged {
		return g.inner.Send(path, message)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.remaining == 0 {
		return errBudget
	}
	if err := g.inner.Send(path, message); err != nil {
		return err
	}
	g.remaining--
	return nil
}

func (g *budgetedCart) left() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.remaining
}
