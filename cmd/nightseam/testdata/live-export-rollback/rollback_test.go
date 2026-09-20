package rollback_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	protocol "example.test/generated/api/go/rollback-protocol"
	"github.com/Bitspark/nightseam/duplex/go"
	"github.com/Bitspark/nightseam/live/go"
	"github.com/Bitspark/nightseam/runtime/go"
)

func TestGeneratedLiveExportRollback(t *testing.T) {
	a, b := duplex.Pipe(8 << 20)
	var sa, sb *live.Scope
	pa, err := runtime.NewPeer(context.Background(), a, runtime.ClientRole, runtime.Options{Prepare: func(p *runtime.Peer) (err error) { sa, err = live.Over(p, live.Options{MaxExports: 2}); return }})
	if err != nil {
		t.Fatal(err)
	}
	defer pa.Close()
	pb, err := runtime.NewPeer(context.Background(), b, runtime.ServerRole, runtime.Options{Prepare: func(p *runtime.Peer) (err error) { sb, err = live.Over(p, live.Options{MaxExports: 4}); return }})
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()
	fn := protocol.Call(func(context.Context) (int64, error) { return 7, nil })
	baseline, err := protocol.ExportCall(sa.Owner(), fn)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := protocol.ImportCall(sb.Owner(), baseline)
	if err != nil {
		t.Fatal(err)
	}
	used := false
	useRaw, err := protocol.ExportUse(sb.Owner(), func(context.Context, []protocol.Call) (int64, error) { used = true; return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	use, err := protocol.ImportUse(sa.Owner(), useRaw)
	if err != nil {
		t.Fatal(err)
	}
	makeRaw, err := protocol.ExportMake(sb.Owner(), func(context.Context) (protocol.Pair, error) { return protocol.Pair{First: fn, Second: fn}, nil })
	if err != nil {
		t.Fatal(err)
	}
	makePair, err := protocol.ImportMake(sa.Owner(), makeRaw)
	if err != nil {
		t.Fatal(err)
	}
	checkRaw, err := protocol.ExportCheck(sb.Owner(), func(context.Context, protocol.Checked) error { used = true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	check, err := protocol.ImportCheck(sa.Owner(), checkRaw)
	if err != nil {
		t.Fatal(err)
	}
	before, remoteBefore := sa.Counts(), sb.Counts()
	for _, name := range []string{"record", "union", "generic", "generic-live", "serialization", "validation", "request", "reply"} {
		t.Run(name, func(t *testing.T) {
			for i := 0; i < 3; i++ {
				var err error
				switch name {
				case "record":
					_, err = protocol.ExportPair(sa.Owner(), protocol.Pair{First: fn, Second: fn})
				case "union":
					_, err = protocol.ExportChoice(sa.Owner(), protocol.Choice{Pair: &protocol.Pair{First: fn, Second: fn}})
				case "generic":
					_, err = protocol.ExportGeneric(sa.Owner(), protocol.Generic{{Item: fn}, {Item: fn}})
				case "generic-live":
					_, err = protocol.ExportBound(sa.Owner(), protocol.Bound[protocol.Call]{First: fn, Last: fn}, func(owner *live.Owner, input protocol.Call) (json.RawMessage, error) {
						return protocol.ExportCall(owner, input)
					}, runtime.TypeBinding{})
				case "serialization":
					_, err = protocol.ExportFailure(sa.Owner(), protocol.Failure{First: fn, Last: json.RawMessage(`{`)})
				case "request":
					_, err = use(context.Background(), []protocol.Call{fn, fn})
				case "reply":
					_, err = makePair(context.Background())
				case "validation":
					err = check(context.Background(), protocol.Checked{First: fn, Last: "bad"})
				}
				if err == nil {
					t.Fatal("invalid export succeeded")
				}
				if name != "serialization" && name != "validation" {
					var public *runtime.PublicError
					if !errors.As(err, &public) || public.Code != live.ErrorTooManyExports {
						t.Fatalf("wrong refusal: %v", err)
					}
				}
				if after := sa.Counts(); after != before {
					t.Fatalf("failed export retained bindings: before %+v, after %+v", before, after)
				}
				if sb.Counts() != remoteBefore {
					t.Fatalf("reply export retained bindings: %+v", sb.Counts())
				}
				if used {
					t.Fatal("failed request reached the handler")
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				value, err := retained(ctx)
				cancel()
				if err != nil || value != 7 {
					t.Fatalf("prior binding invalidated: %d, %v", value, err)
				}
				raw, err := protocol.ExportCall(sa.Owner(), fn)
				if err != nil {
					t.Fatalf("slot was not reusable: %v", err)
				}
				reference, err := sa.Decode(raw)
				if err != nil {
					t.Fatal(err)
				}
				if err := sa.Release(reference); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
