package runtime

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestValidatorDeclarationDigests(t *testing.T) {
	data, err := os.ReadFile("../../conformance/tables/digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct{ Name, Wire, Digest string }
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	rows := map[string]struct{ Wire, Digest string }{}
	for _, row := range table.Cases {
		if _, err := NewSchema([]byte(row.Wire), row.Digest, nil); err != nil {
			t.Fatalf("%s: %v", row.Name, err)
		}
		rows[row.Name] = struct{ Wire, Digest string }{row.Wire, row.Digest}
	}
	first, second := rows["callable revision 1"], rows["callable revision 2"]
	if first.Wire == "" || second.Wire == "" || first.Digest == second.Digest {
		t.Fatal("missing distinct callable revisions")
	}
	for _, revision := range []struct{ Wire, Digest string }{first, second} {
		if err := MustSchema(revision.Wire, revision.Digest, nil).ValidateRaw("Payload", []byte(`{"text":"fits both"}`)); err != nil {
			t.Fatal(err)
		}
	}
	declared := MustSchema(first.Wire, first.Digest, nil)
	consumer := MustSchema(`{"types":{"Box":{"kind":"record","parameters":[{"name":"T"}],"fields":[{"name":"item","type":"T","required":true}]}}}`, second.Digest, map[string]*Schema{"source": declared})
	base := MustSchema(`{"types":{"Holder":{"kind":"record","fields":[{"name":"item","type":"source.Report","required":true}]}}}`, second.Digest, map[string]*Schema{"source": declared})
	inherited := MustSchema(`{"types":{"Holder":{"kind":"record","extends":["base.Holder"]}}}`, second.Digest, map[string]*Schema{"base": base})
	for _, row := range []struct {
		name       string
		schema     *Schema
		expression any
		wrap       bool
	}{
		{"direct", declared, "Report", false},
		{"bound schema", declared.Bind(nil, nil), "Report", false},
		{"imported", consumer, "source.Report", false},
		{"family binding", consumer.Bind(nil, map[string]*Schema{"S": declared}), "S.Report", false},
		{"type binding", consumer.Bind(map[string]any{"T": TypeBinding{Schema: declared, Type: "Report"}}, nil), "T", false},
		{"drawn binding", consumer.Bind(map[string]any{"S.Report": TypeBinding{Schema: declared, Type: "Report"}}, nil), "S.Report", false},
		{"nested application", consumer, map[string]any{"apply": "Box", "with": map[string]any{"T": "source.Report"}}, true},
		{"inherited field", inherited, "Holder", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			for _, digest := range []string{first.Digest, second.Digest, ""} {
				var value any = map[string]any{"binding": "nonce.1", "contract": "same/Report", "digest": digest}
				if row.wrap {
					value = map[string]any{"item": value}
				}
				err := row.schema.ValidateValue(row.expression, value)
				if digest != second.Digest {
					if err != nil {
						t.Fatal(err)
					}
					continue
				}
				var mismatch *PublicError
				if !errors.As(err, &mismatch) || mismatch.Code != "contract_mismatch" || !strings.Contains(mismatch.Message, "same/Report") {
					t.Fatalf("wrong mismatch refusal: %v", err)
				}
			}
		})
	}
	for _, expected := range []string{first.Digest, ""} {
		schema := MustSchema(first.Wire, expected, nil)
		if err := schema.ValidateValue("Report", map[string]any{"binding": "nonce.1", "contract": "same/Report"}); err != nil {
			t.Fatal(err)
		}
		if expected == "" {
			if err := schema.ValidateValue("Report", map[string]any{"binding": "nonce.1", "contract": "same/Report", "digest": second.Digest}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSchemaRefusesMalformedDigest(t *testing.T) {
	for _, digest := range []string{"short", strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("a", 63), strings.Repeat("a", 65)} {
		if _, err := NewSchema([]byte(`{"types":{}}`), digest, nil); err == nil {
			t.Fatalf("accepted malformed schema digest %q", digest)
		}
	}
}
