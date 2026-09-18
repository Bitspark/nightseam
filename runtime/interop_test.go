package wsruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This test exercises the actual TypeScript runtime over a network socket; it
// does not replace either transport with mocks or depend on emitted JS files.
func TestTypeScriptClientInteroperability(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for the TypeScript interoperability acceptance gate")
	}
	_, source, _, _ := runtime.Caller(0)
	driver := filepath.Join(filepath.Dir(source), "..", "..", "ts", "ws-runtime", "src", "interop.ts")
	if _, err := os.Stat(driver); err != nil {
		t.Fatal(err)
	}
	cancelled := make(chan struct{}, 1)
	clientEvents := make(chan json.RawMessage, 1)
	h, err := NewHandler(ServerOptions{
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
		Options: Options{Handlers: map[string]Handler{
			"echo": func(_ context.Context, _ *Peer, params json.RawMessage) (any, error) { return params, nil },
			"roundtrip": func(ctx context.Context, p *Peer, params json.RawMessage) (any, error) {
				var result json.RawMessage
				if err := p.Call(ctx, "multiply", params, &result); err != nil {
					return nil, err
				}
				return result, nil
			},
			"fail": func(context.Context, *Peer, json.RawMessage) (any, error) {
				return nil, &PublicError{Code: "denied", Message: "Access denied"}
			},
			"notify": func(ctx context.Context, p *Peer, _ json.RawMessage) (any, error) {
				return nil, p.Emit(ctx, "notice", map[string]int{"value": 42})
			},
			"wait": func(ctx context.Context, _ *Peer, _ json.RawMessage) (any, error) {
				<-ctx.Done()
				cancelled <- struct{}{}
				return nil, ctx.Err()
			},
		}, Events: map[string]EventHandler{
			"client_notice": func(_ context.Context, _ *Peer, data json.RawMessage) { clientEvents <- data },
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--experimental-strip-types", driver, "ws"+strings.TrimPrefix(server.URL, "http"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("TypeScript interoperability driver failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), `"ok":true`) {
		t.Fatalf("missing driver success: %s", output)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("TypeScript cancellation did not reach Go handler")
	}
	select {
	case data := <-clientEvents:
		var value struct {
			Value int `json:"value"`
		}
		if err := json.Unmarshal(data, &value); err != nil || value.Value != 99 {
			t.Fatalf("unexpected client event: %s (%v)", data, err)
		}
	case <-ctx.Done():
		t.Fatal("TypeScript event did not reach Go")
	}
}
