package main

import (
	"context"
	"fmt"

	binding "example.com/probe/api/go/compose-cell-binding"
	cell "example.com/probe/api/go/compose-cell-protocol"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

type state struct{ value int64 }

func (s *state) Put(_ context.Context, input cell.Put[int64]) (int64, error) {
	s.value = input.Value
	return 1, nil
}
func (s *state) Get(context.Context) (int64, error) { return s.value, nil }

type reverse struct{}

func (reverse) Mirror(_ context.Context, input cell.Put[int64]) (int64, error) {
	return input.Value, nil
}
func (reverse) Changed(context.Context, cell.Put[int64]) error { return nil }

func main() {
	adapter := runtime.JSONAdapter[int64]()
	wire, err := binding.ToWire(func(cell.Client[int64]) (cell.Server[int64], error) {
		return cell.Server[int64]{Methods: &state{}}, nil
	}, runtime.AdapterContext{}, adapter)
	if err != nil {
		panic(err)
	}
	defer wire.Close(duplex.CodeNormal, "")
	model, err := binding.FromWire(context.Background(), wire, runtime.AdapterContext{}, adapter)
	if err != nil {
		panic(err)
	}
	access, err := model(cell.Client[int64]{Methods: reverse{}, Events: reverse{}})
	if err != nil {
		panic(err)
	}
	if n, err := access.Methods.Put(context.Background(), cell.Put[int64]{Value: 42}); err != nil || n != 1 {
		panic(fmt.Sprintf("scalar put: %d %v", n, err))
	}
	if n, err := access.Methods.Get(context.Background()); err != nil || n != 42 {
		panic(fmt.Sprintf("scalar get: %d %v", n, err))
	}
	fmt.Println("packed generic Go scalar Cell: 42 without a live environment")
}
