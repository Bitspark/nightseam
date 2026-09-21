package draws_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	first "example.test/generated/api/go/first-protocol"
	binding "example.test/generated/api/go/holder-binding"
	holder "example.test/generated/api/go/holder-protocol"
	second "example.test/generated/api/go/second-protocol"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

type function = func(context.Context, int64) (int64, error)

type provider[J, P any] struct {
	job      runtime.ValueAdapter[J]
	progress runtime.ValueAdapter[P]
	makeJob  func(function) J
	makeRead func(function) P
	jobRun   func(J) function
	readRun  func(P) function
}

func (p provider[J, P]) value(seed int64) holder.Held[J, P] {
	makeRun := func(offset int64) function {
		return func(_ context.Context, n int64) (int64, error) { return seed + offset + n, nil }
	}
	original := makeRun(0)
	guarded := func(ctx context.Context, n int64) (int64, error) {
		if n < 0 {
			return 0, &runtime.PublicError{Code: "denied", Message: "guarded job refuses negative input"}
		}
		return original(ctx, n)
	}
	return holder.Held[J, P]{
		Job: p.makeJob(guarded), Progress: p.makeRead(makeRun(1)),
		Nested: holder.Nested[J, P]{
			Jobs: []J{p.makeJob(makeRun(2))},
			Progress: map[string]runtime.Nullable[P]{
				"value": runtime.NonNull(p.makeRead(makeRun(3))), "none": runtime.Null[P](),
			},
			Choice: holder.Choice[J]{Job: &holder.ChoiceJobValue[J]{Value: p.makeJob(makeRun(4))}},
		},
	}
}

func (p provider[J, P]) observe(ctx context.Context, value holder.Held[J, P]) ([]int64, error) {
	if len(value.Nested.Jobs) != 1 || value.Nested.Choice.Job == nil || value.Nested.Absent.Present || !value.Nested.Progress["none"].Null {
		return nil, errors.New("nested shape, null or absent value changed")
	}
	functions := []function{
		p.jobRun(value.Job), p.readRun(value.Progress), p.jobRun(value.Nested.Jobs[0]),
		p.readRun(value.Nested.Progress["value"].Value), p.jobRun(value.Nested.Choice.Job.Value),
	}
	var observations []int64
	for _, invoke := range functions {
		n, err := invoke(ctx, 1)
		if err != nil {
			return nil, err
		}
		observations = append(observations, n)
	}
	_, err := functions[0](ctx, -1)
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != "denied" {
		return nil, fmt.Errorf("guard was not preserved: %v", err)
	}
	return observations, nil
}

func exported[T any](owner *live.Owner, adapter runtime.ValueAdapter[T], value T) (json.RawMessage, error) {
	ctx := live.WithOwner(context.Background(), owner)
	return live.ValueEnvironment(owner.Scope()).Export(ctx, func(ctx context.Context) (json.RawMessage, error) {
		return adapter.Export(ctx, value)
	})
}

func imported[T any](owner *live.Owner, adapter runtime.ValueAdapter[T], raw json.RawMessage) (T, error) {
	var value T
	ctx := live.WithOwner(context.Background(), owner)
	err := live.ValueEnvironment(owner.Scope()).Import(ctx, func(ctx context.Context) error {
		converted, err := adapter.Import(ctx, raw)
		if err == nil {
			value = converted
		}
		return err
	})
	return value, err
}

func awaitCounts(ctx context.Context, scope *live.Scope, want live.Counts) error {
	for scope.Counts() != want {
		select {
		case <-ctx.Done():
			return fmt.Errorf("counts %+v, want %+v: %w", scope.Counts(), want, ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	return nil
}

func wireCounts(counts live.Counts) map[string]int {
	return map[string]int{"exports": counts.Exports, "imports": counts.Imports}
}

type session[J, P any] struct {
	provider[J, P]
	scope              *live.Scope
	adapter            runtime.ValueAdapter[holder.Held[J, P]]
	exporter, receiver *live.Owner
	borrowed           holder.Held[J, P]
	modelOwners        []*live.Owner
	complete           func(context.Context) (holder.ServerModel[J, P], error)
	modelCalls         atomic.Int64
}

func (s *session[J, P]) Exchange(ctx context.Context, value holder.Held[J, P]) (holder.Held[J, P], error) {
	owner, ok := live.OwnerOf(ctx)
	if !ok || owner == nil {
		return value, errors.New("generated handler received no active owner")
	}
	s.modelOwners = append(s.modelOwners, owner)
	s.modelCalls.Add(1)
	return value, nil
}

func (s *session[J, P]) drop() {
	for _, owner := range s.modelOwners {
		_ = owner.Release()
	}
	s.modelOwners = nil
	for _, owner := range []*live.Owner{s.exporter, s.receiver} {
		if owner != nil {
			_ = owner.Release()
		}
	}
	s.exporter, s.receiver = nil, nil
}

func (s *session[J, P]) controls(peer *runtime.Peer) error {
	methods := map[string]runtime.Handler{
		"test.counts": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) {
			return wireCounts(s.scope.Counts()), nil
		},
		"test.drop": func(context.Context, *runtime.Peer, json.RawMessage) (any, error) { s.drop(); return nil, nil },
		"test.export": func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
			var input struct{ Seed int64 }
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			if s.exporter == nil {
				s.exporter = s.scope.Owner().Child()
			}
			return exported(s.exporter, s.adapter, s.value(input.Seed))
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
			value, err := imported(owner, s.adapter, input.Value)
			if input.Failure {
				var public *runtime.PublicError
				if !errors.As(err, &public) || public.Code != live.ErrorTooManyImports {
					return nil, fmt.Errorf("expected partial import capacity refusal: %v", err)
				}
				if owner.Counts() != (live.Counts{}) || s.receiver.Counts().Imports != 5 || s.scope.Counts().Imports != 5 {
					return nil, fmt.Errorf("partial import leaked or removed borrowed aliases: %+v %+v %+v", owner.Counts(), s.receiver.Counts(), s.scope.Counts())
				}
			} else {
				if err != nil {
					return nil, err
				}
				s.borrowed = value
			}
			seen, err := s.observe(ctx, s.borrowed)
			return map[string]any{"accepted": !input.Failure, "observed": seen, "counts": wireCounts(s.scope.Counts()), "owner": wireCounts(owner.Counts())}, err
		},
		"test.bad_export": func(ctx context.Context, _ *runtime.Peer, _ json.RawMessage) (any, error) {
			before := s.scope.Counts()
			owner := s.scope.Owner().Child()
			defer owner.Release()
			bad := s.borrowed
			bad.Progress = s.makeRead(nil)
			if _, err := exported(owner, s.adapter, bad); err == nil {
				return nil, errors.New("partially valid export was accepted")
			}
			if owner.Counts() != (live.Counts{}) || s.scope.Counts() != before {
				return nil, errors.New("partial export leaked or released borrowed imports")
			}
			seen, err := s.observe(ctx, s.borrowed)
			return map[string]any{"observed": seen, "counts": wireCounts(before)}, err
		},
		"test.exercise": func(ctx context.Context, _ *runtime.Peer, _ json.RawMessage) (any, error) {
			if s.complete == nil {
				return nil, errors.New("wrong generated role")
			}
			model, err := s.complete(ctx)
			if err != nil {
				return nil, err
			}
			access, err := model(holder.Client[J, P]{})
			if err != nil {
				return nil, err
			}
			owner := s.scope.Owner().Child()
			value, err := access.Methods.Exchange(live.WithOwner(ctx, owner), s.value(100))
			if err != nil {
				return nil, err
			}
			seen, err := s.observe(ctx, value)
			if err != nil {
				return nil, err
			}
			if !reflect.DeepEqual(seen, []int64{101, 102, 103, 104, 105}) {
				return nil, fmt.Errorf("returned callbacks changed: %v", seen)
			}
			if owner.Counts() != (live.Counts{Exports: 5, Imports: 5}) || s.scope.Owner().Counts() != (live.Counts{}) {
				return nil, fmt.Errorf("generated operation selected the wrong owner: %+v", owner.Counts())
			}
			_ = owner.Release()
			var ignored any
			if err := peer.Call(ctx, "test.drop", struct{}{}, &ignored); err != nil {
				return nil, err
			}
			if err := awaitCounts(ctx, s.scope, live.Counts{}); err != nil {
				return nil, err
			}
			return map[string]any{"observed": seen, "released": wireCounts(s.scope.Counts())}, nil
		},
	}
	for name, handler := range methods {
		if err := peer.Handle(name, handler); err != nil {
			return err
		}
	}
	return nil
}

func runProvider[J runtime.Of[Tag], P runtime.Of[Tag], Tag any](t *testing.T, name string, p provider[J, P]) {
	// The same interpretations are constructed once, outside both live scopes.
	adapter := holder.AdapterHeld(p.job, p.progress)
	for _, role := range []string{"go-server", "typescript-server"} {
		t.Run(role, func(t *testing.T) {
			var connections atomic.Int64
			handler, err := runtime.NewHandler(runtime.ServerOptions{
				Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
				CheckOrigin:  func(*http.Request) bool { return true },
				Options: runtime.Options{Prepare: func(peer *runtime.Peer) error {
					scope, err := live.Over(peer, live.Options{MaxImports: 7, MaxExports: 64})
					if err != nil {
						return err
					}
					s := &session[J, P]{provider: p, scope: scope, adapter: adapter}
					if err := s.controls(peer); err != nil {
						return err
					}
					connections.Add(1)
					if role == "go-server" {
						wire, err := binding.ToWire(func(holder.Client[J, P]) (holder.Server[J, P], error) { return holder.Server[J, P]{Methods: s}, nil }, runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)}, p.job, p.progress)
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
						s.complete, cleanup, err = binding.PrepareFromWire(peer.Wire(), runtime.AdapterContext{ValueEnvironment: live.ValueEnvironment(scope)}, p.job, p.progress)
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
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "node", "--loader", "./runtime-loader.mjs", "sockets.ts", "ws"+strings.TrimPrefix(server.URL, "http"), name, role)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("GEN-BIND-%s-%s: %v\n%s", name, role, err, output)
			}
			if connections.Load() != 2 {
				t.Fatalf("used %d physical scopes, want two", connections.Load())
			}
		})
	}
}

func TestGENBINDFirstProvider(t *testing.T) {
	runProvider(t, "first", provider[first.Job, first.Progress]{first.AdapterJob(), first.AdapterProgress(), func(fn function) first.Job { return first.Job{Run: fn} }, func(fn function) first.Progress { return first.Progress{Read: fn} }, func(v first.Job) function { return v.Run }, func(v first.Progress) function { return v.Read }})
}

func TestGENBINDSecondProvider(t *testing.T) {
	runProvider(t, "second", provider[second.Job, second.Progress]{second.AdapterJob(), second.AdapterProgress(), func(fn function) second.Job { return second.Job{Run: fn} }, func(fn function) second.Progress { return second.Progress{Read: fn} }, func(v second.Job) function { return v.Run }, func(v second.Progress) function { return v.Read }})
}
