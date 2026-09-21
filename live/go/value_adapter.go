package live

import (
	"context"
	"encoding/json"
	"github.com/Bitspark/nightseam/runtime/go"
)

// ValueEnvironment binds conversion effects to a scope, never to a permanent
// owner. Each operation supplies its selected owner through WithOwner; without
// a same-scope owner, Select uses the scope's current root lifetime.
func ValueEnvironment(scope *Scope) runtime.ValueEnvironment {
	return &valueEnvironment{scope: scope}
}

type valueEnvironment struct{ scope *Scope }

func (e *valueEnvironment) Select(ctx context.Context) (context.Context, error) {
	if ctx == nil || e.scope == nil {
		return nil, &runtime.PublicError{Code: ErrorScopeClosed, Message: "live conversion requires a context and scope"}
	}
	owner, ok := OwnerOf(ctx)
	if !ok || owner.Scope() != e.scope.root {
		owner = e.scope.Owner()
	}
	return WithOwner(ctx, owner), nil
}
func (e *valueEnvironment) Child(ctx context.Context) (context.Context, error) {
	ctx, err := e.Select(ctx)
	if err != nil {
		return nil, err
	}
	owner, _ := OwnerOf(ctx)
	return WithOwner(ctx, owner.Child()), nil
}
func (e *valueEnvironment) Export(ctx context.Context, build func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	ctx, err := e.Select(ctx)
	if err != nil {
		return nil, err
	}
	owner, _ := OwnerOf(ctx)
	return owner.ExportValue(func(view *Owner) (json.RawMessage, error) { return build(WithOwner(ctx, view)) })
}
func (e *valueEnvironment) Import(ctx context.Context, build func(context.Context) error) error {
	ctx, err := e.Select(ctx)
	if err != nil {
		return err
	}
	owner, _ := OwnerOf(ctx)
	return owner.ImportValue(func(view *Owner) error { return build(WithOwner(ctx, view)) })
}
func (e *valueEnvironment) Publish(ctx context.Context, build func(context.Context) (json.RawMessage, error), publish func(json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error) {
	ctx, err := e.Select(ctx)
	if err != nil {
		return nil, err
	}
	owner, _ := OwnerOf(ctx)
	return owner.PublishValue(func(view *Owner) (json.RawMessage, error) { return build(WithOwner(ctx, view)) }, publish)
}
