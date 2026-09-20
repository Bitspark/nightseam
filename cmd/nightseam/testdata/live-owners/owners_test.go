package owners_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	combinator "example.test/generated/api/go/combinator-protocol"
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
