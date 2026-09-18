package runtime

import (
	"encoding/json"
	"testing"
)

func TestOptionalNullableDistinguishesAbsentNullAndZero(t *testing.T) {
	type record struct {
		Value Optional[Nullable[int64]] `json:"value,omitzero"`
	}
	for _, example := range []struct {
		json    string
		present bool
		null    bool
		value   int64
	}{
		{`{}`, false, false, 0},
		{`{"value":null}`, true, true, 0},
		{`{"value":0}`, true, false, 0},
		{`{"value":42}`, true, false, 42},
	} {
		t.Run(example.json, func(t *testing.T) {
			var got record
			if err := json.Unmarshal([]byte(example.json), &got); err != nil {
				t.Fatal(err)
			}
			if got.Value.Present != example.present || got.Value.Value.Null != example.null || got.Value.Value.Value != example.value {
				t.Fatalf("unexpected decoded value: %#v", got)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != example.json {
				t.Fatalf("round trip = %s, want %s", encoded, example.json)
			}
		})
	}
	encoded, err := json.Marshal(record{Value: Some(NonNull[int64](0))})
	if err != nil || string(encoded) != `{"value":0}` {
		t.Fatalf("constructed zero: %s %v", encoded, err)
	}
}

func TestFrameShapeRejectsAmbiguousAndForeignMembers(t *testing.T) {
	valid := []string{
		`{"version":1,"kind":"request","id":"c:1","method":"read","params":null}`,
		`{"version":1,"kind":"response","id":"c:1","result":null}`,
		`{"version":1,"kind":"response","id":"c:1","error":{"code":"busy","message":"Try later"}}`,
		`{"version":1,"kind":"event","event":"updated","data":null}`,
		`{"version":1,"kind":"cancel","id":"c:1"}`,
	}
	for _, data := range valid {
		if _, err := decodeFrame([]byte(data)); err != nil {
			t.Errorf("rejected valid frame %s: %v", data, err)
		}
	}
	invalid := []string{
		`{"version":1,"kind":"request","id":"c:1","method":"read"}`,
		`{"version":1,"kind":"response","id":"c:1","result":null,"error":null}`,
		`{"version":1,"kind":"response","id":"c:1","error":null}`,
		`{"version":1,"kind":"event","event":"updated","data":null,"id":""}`,
		`{"version":1,"kind":"cancel","id":"c:1","event":""}`,
		`{"version":1,"version":1,"kind":"cancel","id":"c:1"}`,
		`{"version":2,"kind":"cancel","id":"c:1"}`,
		`{"version":1,"kind":"cancel","id":"c:1"} {}`,
		`null`, `[]`, `{"version":1,"kind":"unknown"}`,
	}
	for _, data := range invalid {
		if _, err := decodeFrame([]byte(data)); err == nil {
			t.Errorf("accepted invalid frame %s", data)
		}
	}
}
