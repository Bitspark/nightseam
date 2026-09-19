package load

import (
	"strings"
	"testing"
)

func TestMalformedUnicodeIsRefusedAtItsSourceFile(t *testing.T) {
	for _, file := range []string{"model.json", "protocol.json", "session.json", "go.json"} {
		for _, raw := range []string{`{"description":"\uD800"}`, `{"\uDC00":null}`, "{\"description\":\"" + string([]byte{0xff}) + "\"}"} {
			files := map[string]string{"probe/model.json": probeModel, "probe/protocol.json": `{"profile":"nightseam.duplex/1"}`, "probe/" + file: raw}
			_, problems := Checkout(checkout(files), "api/contracts", []string{"go"})
			found := false
			for _, problem := range problems {
				if problem.Code == "invalid_json" && problem.File == file && strings.Contains(problem.Message, "Unicode scalar") {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s: lost source/refusal: %v", file, problems)
			}
		}
	}
}
