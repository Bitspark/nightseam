package livetest

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"

	"github.com/Bitspark/nightseam/live/go"
)

const digestOne = "68025e009ca0e1d178ab57548a507c65dcaefb7b62f3867207b26fdbe5739cb0"
const digestTwo = "ccc90bf9332f9595615ec1c0c34627a7cc52779378f7ca00e56d6de7e89955fb"

func digestCases() []Case {
	return []Case{
		{"a reference revision is checked before attachment", referenceDigest},
		{"an absent reference digest does not refuse an import", absentDigest},
		{"an attachment retains its strongest known digest", attachmentDigest},
		{"an exported binding refuses a forged revision", exportedDigest},
		{"malformed reference digests allocate nothing", malformedDigest},
		{"forwarding preserves a declared digest", forwardedDigest},
		{"a digest mismatch unwinds only fresh imports", digestRollback},
	}
}

func digestReference(t T, scope *live.Scope, reference live.Reference, digest *string) live.Reference {
	t.Helper()
	raw, err := json.Marshal(reference)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("wire: %v", err)
	}
	if digest != nil {
		wire["digest"] = *digest
	}
	raw, _ = json.Marshal(wire)
	arrived, err := scope.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return arrived
}

func digestExport(t T, owner *live.Owner, digest string, invoke live.Invoke) live.Reference {
	t.Helper()
	ref, err := owner.Export(sink, digest, invoke)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if ref.Digest() != digest {
		t.Fatalf("reference digest = %q, want %q", ref.Digest(), digest)
	}
	return ref
}

func digestRelease(t T, p Pair) {
	p.A.Owner().Release()
	p.B.Owner().Release()
	holds(t, p.A, 0, 0, "released exporter")
	holds(t, p.B, 0, 0, "released importer")
}

func referenceDigest(t T, p Pair) {
	var called atomic.Int32
	ref := digestExport(t, p.A.Owner(), digestOne, func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		called.Add(1)
		return echo(ctx, raw)
	})
	arrived := digestReference(t, p.B, ref, nil)
	_, err := p.B.Owner().Import(arrived, sink, digestTwo)
	refused(t, err, live.ErrorContractMismatch)
	if err != nil && !strings.Contains(err.Error(), sink) {
		t.Errorf("mismatch did not name %s: %v", sink, err)
	}
	holds(t, p.B, 0, 0, "refused revision")
	if called.Load() != 0 {
		t.Errorf("refused import invoked the implementation")
	}
	invoke, err := p.B.Owner().Import(arrived, sink, digestOne)
	if err != nil {
		t.Fatalf("same revision: %v", err)
	}
	if got := call(t, invoke, "41"); string(got) != "41" {
		t.Errorf("same revision returned %s", got)
	}
	digestRelease(t, p)
}

func absentDigest(t T, p Pair) {
	for _, digests := range [][2]string{{"", digestOne}, {digestOne, ""}, {"", ""}} {
		ref := digestExport(t, p.A.Owner(), digests[0], echo)
		raw, _ := json.Marshal(ref)
		var wire map[string]any
		_ = json.Unmarshal(raw, &wire)
		if _, present := wire["digest"]; present != (digests[0] != "") {
			t.Errorf("digest presence in %s", raw)
		}
		invoke, err := p.B.Owner().Import(digestReference(t, p.B, ref, nil), sink, digests[1])
		if err != nil {
			t.Fatalf("absent digest: %v", err)
		}
		call(t, invoke, "42")
	}
	digestRelease(t, p)
}

func attachmentDigest(t T, p Pair) {
	ref := digestExport(t, p.A.Owner(), "", echo)
	arrived := digestReference(t, p.B, ref, nil)
	if _, err := p.B.Owner().Import(arrived, sink, ""); err != nil {
		t.Fatalf("unspecified: %v", err)
	}
	known := digestReference(t, p.B, ref, ptrDigest(digestOne))
	if _, err := p.B.Owner().Import(known, sink, ""); err != nil {
		t.Fatalf("narrowing: %v", err)
	}
	if arrived.Digest() != "" {
		t.Errorf("narrowing rewrote the received reference")
	}
	if _, err := p.B.Owner().Import(arrived, sink, ""); err != nil {
		t.Fatalf("absent alias: %v", err)
	}
	conflict := digestReference(t, p.B, ref, ptrDigest(digestTwo))
	_, err := p.B.Owner().Import(conflict, sink, "")
	refused(t, err, live.ErrorContractMismatch)
	_, err = p.B.Owner().Import(arrived, sink, digestTwo)
	refused(t, err, live.ErrorContractMismatch)
	holds(t, p.B, 0, 1, "conflicting aliases")
	// A supplied expected revision also constrains a reference that carries none.
	second := digestExport(t, p.A.Owner(), "", echo)
	secondArrived := digestReference(t, p.B, second, nil)
	if _, err := p.B.Owner().Import(secondArrived, sink, digestOne); err != nil {
		t.Fatalf("expected revision: %v", err)
	}
	_, err = p.B.Owner().Import(secondArrived, sink, digestTwo)
	refused(t, err, live.ErrorContractMismatch)
	digestRelease(t, p)
}

func ptrDigest(value string) *string { return &value }

func exportedDigest(t T, p Pair) {
	ref := digestExport(t, p.A.Owner(), digestOne, echo)
	forged := digestReference(t, p.A, ref, ptrDigest(digestTwo))
	if ref == forged {
		t.Errorf("references to different revisions compare equal")
	}
	_, err := p.A.Owner().Import(forged, sink, digestTwo)
	refused(t, err, live.ErrorContractMismatch)
	_, err = p.A.Owner().Import(forged, sink, "")
	refused(t, err, live.ErrorContractMismatch)
	_, err = p.B.Owner().Import(ref, sink, digestOne)
	refused(t, err, live.ErrorReferenceForeign)
	holds(t, p.A, 1, 0, "forged own revision")
	holds(t, p.B, 0, 0, "foreign same revision")
	digestRelease(t, p)
}

func malformedDigest(t T, p Pair) {
	for _, invalid := range []string{"short", strings.Repeat("A", 64), strings.Repeat("g", 64), digestOne + "0", digestOne + "\n"} {
		_, err := p.A.Owner().Export(sink, invalid, echo)
		refused(t, err, live.ErrorContractInvalid)
		ref, err := p.B.Decode(json.RawMessage(`{"binding":"other.1","contract":"probe/Report"}`))
		if err != nil {
			t.Fatalf("decode absence: %v", err)
		}
		_, err = p.B.Owner().Import(ref, sink, invalid)
		refused(t, err, live.ErrorContractInvalid)
	}
	for _, invalid := range []string{`""`, `null`, `true`, `7`, `[]`, `"short"`, `"` + strings.Repeat("A", 64) + `"`, `"` + digestOne + `\n"`} {
		_, err := p.B.Decode(json.RawMessage(`{"binding":"other.1","contract":"probe/Report","digest":` + invalid + `}`))
		refused(t, err, live.ErrorContractInvalid)
	}
	holds(t, p.A, 0, 0, "invalid exports")
	holds(t, p.B, 0, 0, "invalid imports")
}

func forwardedDigest(t T, p Pair) {
	ref := digestExport(t, p.A.Owner(), digestOne, echo)
	invoke, err := p.B.Owner().Import(digestReference(t, p.B, ref, nil), sink, digestOne)
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	forwarded, err := live.Forward(p.B.Owner(), sink, digestOne, invoke)
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if forwarded.Digest() != digestOne {
		t.Errorf("forward lost its digest")
	}
	through, err := p.A.Owner().Import(digestReference(t, p.A, forwarded, nil), sink, digestOne)
	if err != nil {
		t.Fatalf("forward import: %v", err)
	}
	call(t, through, "43")
	digestRelease(t, p)
}

func digestRollback(t T, p Pair) {
	owner := p.B.Owner().Child()
	prior := digestReference(t, p.B, digestExport(t, p.A.Owner(), digestOne, echo), nil)
	freshRef := digestReference(t, p.B, digestExport(t, p.A.Owner(), digestOne, echo), nil)
	wrong := digestReference(t, p.B, digestExport(t, p.A.Owner(), digestTwo, echo), nil)
	retained, err := owner.Import(prior, sink, digestOne)
	if err != nil {
		t.Fatalf("prior import: %v", err)
	}
	var fresh live.Invoke
	err = owner.ImportValue(func(batch *live.Owner) error {
		var err error
		fresh, err = batch.Import(freshRef, sink, digestOne)
		if err != nil {
			return err
		}
		_, err = batch.Import(wrong, sink, digestOne)
		return err
	})
	refused(t, err, live.ErrorContractMismatch)
	holds(t, p.B, 0, 1, "digest rollback preserved the prior import")
	call(t, retained, "44")
	if fresh == nil {
		t.Fatalf("batch did not reach its fresh import")
	}
	ctx, cancel := ctx()
	defer cancel()
	_, err = fresh(ctx, json.RawMessage("45"))
	refused(t, err, live.ErrorReferenceReleased)
	digestRelease(t, p)
}
