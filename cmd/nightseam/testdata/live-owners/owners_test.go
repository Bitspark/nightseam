package owners_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	boxes "example.test/generated/api/go/boxes-protocol"
	combinator "example.test/generated/api/go/combinator-protocol"
	ownerclient "example.test/generated/api/go/owners-client"
	owners "example.test/generated/api/go/owners-protocol"
	worker "example.test/generated/api/go/worker-protocol"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

func pair(t *testing.T, imports int) (*live.Scope, *live.Scope) {
	t.Helper()
	a, b := duplex.Pipe(1 << 20)
	var sa, sb *live.Scope
	pa, err := runtime.NewPeer(context.Background(), a, runtime.ClientRole, runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
		sa, err = live.Over(p, live.Options{MaxExports: 4, MaxImports: 4})
		return
	}})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := runtime.NewPeer(context.Background(), b, runtime.ServerRole, runtime.Options{Prepare: func(p *runtime.Peer) (err error) {
		sb, err = live.Over(p, live.Options{MaxExports: 4, MaxImports: imports})
		return
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pa.Close(); _ = pb.Close() })
	return sa, sb
}
func zero(t *testing.T, scopes ...*live.Scope) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for _, s := range scopes {
		for s.Counts() != (live.Counts{}) {
			if time.Now().After(end) {
				t.Fatalf("owner release leaked %+v", s.Counts())
			}
			time.Sleep(time.Millisecond)
		}
	}
}
func release(t *testing.T, owner *live.Owner) {
	t.Helper()
	if err := owner.Release(); err != nil {
		t.Fatal(err)
	}
}
func refusal(t *testing.T, err error, code string) {
	t.Helper()
	var public *runtime.PublicError
	if !errors.As(err, &public) || public.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestNestedAndGenericOwnerConversions(t *testing.T) {
	sa, sb := pair(t, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 7; i++ {
		a, b := sa.Owner().Child(), sb.Owner().Child()
		job := worker.Job{Ticket: "job", Cancel: func(context.Context) error { return nil }}
		raw, err := owners.ExportJobs(a, owners.Jobs{Items: []worker.Job{job, job}})
		if err != nil {
			t.Fatal(err)
		}
		page, err := owners.ImportJobs(b, raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, job := range page.Items {
			if err := job.Cancel(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if sa.Counts().Exports != 2 || sb.Counts().Imports != 2 {
			t.Fatal("native identity incorrectly deduplicated independent exports")
		}
		release(t, b)
		refusal(t, page.Items[0].Cancel(ctx), live.ErrorReferenceReleased)
		release(t, a)
		zero(t, sa, sb)
		a, b = sa.Owner().Child(), sb.Owner().Child()
		bundle := combinator.Bundle[worker.Job]{Metadata: combinator.BundleMetadata[worker.Job]{Seed: job}, Run: func(_ context.Context, n combinator.Count) (combinator.Count, error) { return n + 1, nil }}
		raw, err = combinator.ExportBundle(a, bundle, worker.ExportJob, job.WireType())
		if err != nil {
			t.Fatal(err)
		}
		got, err := combinator.ImportBundle(b, raw, worker.ImportJob, job.WireType())
		if err != nil {
			t.Fatal(err)
		}
		if err := got.Metadata.Seed.Cancel(ctx); err != nil {
			t.Fatal(err)
		}
		if n, err := got.Run(ctx, 3); err != nil || n != 4 {
			t.Fatalf("generic callable: %d %v", n, err)
		}
		release(t, b)
		release(t, a)
		zero(t, sa, sb)
	}
}

func TestGenericImportBatchOwnsConverterAcquisitions(t *testing.T) {
	sa, sb := pair(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, borrow := range []bool{false, true} {
		exporter := sa.Owner().Child()
		bundle := combinator.Bundle[worker.Report]{Metadata: combinator.BundleMetadata[worker.Report]{Seed: func(context.Context, worker.Percent) error { return nil }}, Run: func(_ context.Context, n combinator.Count) (combinator.Count, error) { return n, nil }}
		typeReport := runtime.TypeBinding{Schema: worker.WireSchema(), Type: "Report"}
		raw, err := combinator.ExportBundle(exporter, bundle, worker.ExportReport, typeReport)
		if err != nil {
			t.Fatal(err)
		}
		held, batch := sb.Owner().Child(), sb.Owner().Child()
		var retained worker.Report
		if borrow {
			var object struct {
				Metadata struct {
					Seed json.RawMessage `json:"seed"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatal(err)
			}
			retained, err = worker.ImportReport(held, object.Metadata.Seed)
			if err != nil {
				t.Fatal(err)
			}
		}
		_, err = combinator.ImportBundle(batch, raw, worker.ImportReport, typeReport)
		refusal(t, err, live.ErrorTooManyImports)
		if batch.Counts() != (live.Counts{}) {
			t.Fatalf("failed converter retained %+v", batch.Counts())
		}
		want := 0
		if borrow {
			want = 1
		}
		if sb.Counts().Imports != want {
			t.Fatalf("failed converter changed held attachments: %+v", sb.Counts())
		}
		release(t, batch)
		if borrow {
			if err := retained(ctx, 7); err != nil {
				t.Fatalf("borrowed attachment invalidated: %v", err)
			}
		}
		release(t, held)
		release(t, exporter)
		zero(t, sa, sb)
	}
}

func TestGeneratedCallableOwnerReleaseIsABarrier(t *testing.T) {
	for _, side := range []string{"exporter", "caller", "both"} {
		t.Run(side, func(t *testing.T) {
			sa, sb := pair(t, 4)
			exporter, caller := sa.Owner().Child(), sb.Owner().Child()
			entered, resume := make(chan struct{}), make(chan struct{})
			raw, err := combinator.ExportUnary(exporter, func(ctx context.Context, value combinator.Count) (combinator.Count, error) {
				close(entered)
				select {
				case <-resume:
					return value + 1, nil
				case <-ctx.Done():
					return 0, ctx.Err()
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			invoke, err := combinator.ImportUnary(caller, raw)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			type result struct {
				value combinator.Count
				err   error
			}
			settled := make(chan result, 1)
			go func() { value, err := invoke(live.WithOwner(ctx, caller), 8); settled <- result{value, err} }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("generated callback never entered")
			}
			if side == "exporter" || side == "both" {
				release(t, exporter)
			}
			if side == "caller" || side == "both" {
				release(t, caller)
			}
			close(resume)
			select {
			case got := <-settled:
				if got.err != nil || got.value != 9 {
					t.Fatalf("release changed an already dispatched scalar result: value=%d error=%v", got.value, got.err)
				}
			case <-ctx.Done():
				t.Fatal("released callback did not settle")
			}
			_, err = invoke(ctx, 9)
			refusal(t, err, live.ErrorReferenceReleased)
			release(t, caller)
			release(t, exporter)
			zero(t, sa, sb)
		})
	}
}

func TestGeneratedOwnerSelectionIsConnectionLocal(t *testing.T) {
	for _, path := range []string{"callable", "operation"} {
		for _, selection := range []string{"foreign", "released-foreign", "local"} {
			t.Run(path+"/"+selection, func(t *testing.T) {
				sa, sb := pair(t, 4)
				var invoke combinator.Factory
				if path == "callable" {
					raw, err := combinator.ExportFactory(sa.Owner(), func(_ context.Context, input combinator.Unary) (combinator.Unary, error) { return input, nil })
					if err != nil {
						t.Fatal(err)
					}
					invoke, err = combinator.ImportFactory(sb.Owner().Child(), raw)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					// The fixture's server uses generated converters. The call under
					// test uses the ordinary generated client and its owner selection.
					serverOwner := sa.Owner().Child()
					if err := sa.Peer().Handle("pack", func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
						var value struct {
							Item json.RawMessage `json:"item"`
						}
						if err := json.Unmarshal(raw, &value); err != nil {
							return nil, err
						}
						input, err := combinator.ImportUnary(serverOwner, value.Item)
						if err != nil {
							return nil, err
						}
						run, err := combinator.ExportUnary(serverOwner, input)
						if err != nil {
							return nil, err
						}
						return map[string]any{"metadata": map[string]any{"seed": 7}, "run": run}, nil
					}); err != nil {
						t.Fatal(err)
					}
					client := &ownerclient.Client{Peer: sb.Peer()}
					invoke = func(ctx context.Context, input combinator.Unary) (combinator.Unary, error) {
						bundle, err := client.Pack(ctx, boxes.Box[combinator.Unary]{Item: input})
						return bundle.Run, err
					}
				}
				foreign := sa.Owner().Child()
				supplied, selected := foreign, sb.Owner()
				if selection == "released-foreign" {
					release(t, foreign)
				}
				if selection == "local" {
					selected = sb.Owner().Child()
					supplied = selected
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				callback := combinator.Unary(func(_ context.Context, value combinator.Count) (combinator.Count, error) { return value + 2, nil })
				returned, err := invoke(live.WithOwner(ctx, supplied), callback)
				if err != nil {
					t.Fatalf("connection-local owner selection: %v", err)
				}
				if got := selected.Counts(); got != (live.Counts{Exports: 1, Imports: 1}) {
					t.Fatalf("request and result allocated outside selected owner: %+v", got)
				}
				if got := foreign.Counts(); got != (live.Counts{}) {
					t.Fatalf("foreign owner acquired connection-local bindings: %+v", got)
				}
				if selection == "local" && sb.Owner().Counts() != (live.Counts{}) {
					t.Fatalf("narrow override leaked allocations to root: %+v", sb.Owner().Counts())
				}
				if value, err := returned(ctx, 10); err != nil || value != 12 {
					t.Fatalf("returned callback: %d %v", value, err)
				}
				release(t, selected)
				if got := selected.Counts(); got != (live.Counts{}) {
					t.Fatalf("released selection retained %+v", got)
				}
				_, err = returned(ctx, 10)
				refusal(t, err, live.ErrorReferenceReleased)
				_, err = invoke(live.WithOwner(ctx, selected), callback)
				refusal(t, err, live.ErrorReferenceReleased)
				release(t, sb.Owner())
				release(t, sa.Owner())
				zero(t, sa, sb)
			})
		}
	}
}
