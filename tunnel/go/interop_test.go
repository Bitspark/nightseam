package tunnel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	duplexruntime "github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/tunnel/go"
)

// TestTypeScriptTunnelInteroperability holds the two tunnels to each other
// over a real WebSocket: the TypeScript side opens a channel and calls a
// method Go serves over it; Go opens a channel and calls a method the
// TypeScript side serves over it; both resume from the sequence the opener
// said.
func TestTypeScriptTunnelInteroperability(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required for the TypeScript interoperability gate")
	}
	_, source, _, _ := runtime.Caller(0)
	driver := filepath.Join(filepath.Dir(source), "..", "ts", "src", "interop.ts")
	if _, err := os.Stat(driver); err != nil {
		t.Fatal(err)
	}
	failures := make(chan error, 4)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := duplexruntime.Accept(w, r, duplexruntime.ServerOptions{Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil }, CheckOrigin: func(*http.Request) bool { return true }})
		if err != nil {
			return
		}
		ctx := peer.Context()
		tn, err := tunnel.New(peer, tunnel.Options{})
		if err != nil {
			failures <- err
			return
		}
		inbound, err := tn.Accept(ctx)
		if err != nil {
			failures <- err
			return
		}
		if inbound.Family != "probe" || inbound.After != 3 || inbound.ID%2 != 1 {
			failures <- fmt.Errorf("accepted %+v", inbound)
			return
		}
		served, err := duplexruntime.NewPeer(ctx, inbound, duplexruntime.ServerRole, duplexruntime.Options{Handlers: map[string]duplexruntime.Handler{
			"echo": func(_ context.Context, _ *duplexruntime.Peer, params json.RawMessage) (any, error) {
				return params, nil
			},
		}})
		if err != nil {
			failures <- err
			return
		}
		defer served.Close()
		outbound, err := tn.Open(ctx, "codex", 11)
		if err != nil {
			failures <- err
			return
		}
		caller, err := duplexruntime.NewPeer(ctx, outbound, duplexruntime.ClientRole, duplexruntime.Options{})
		if err != nil {
			failures <- err
			return
		}
		defer caller.Close()
		var result struct {
			Value int `json:"value"`
		}
		if err := caller.Call(ctx, "multiply", map[string]int{"value": 7}, &result); err != nil {
			failures <- err
			return
		}
		if result.Value != 21 {
			failures <- fmt.Errorf("multiplied to %d", result.Value)
			return
		}
		if err := peer.Emit(ctx, "done", map[string]bool{"ok": true}); err != nil {
			failures <- err
			return
		}
		<-peer.Done()
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--experimental-strip-types", driver, "ws"+strings.TrimPrefix(server.URL, "http"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("TypeScript tunnel driver failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), `"ok":true`) {
		t.Fatalf("missing driver success: %s", output)
	}
	select {
	case err := <-failures:
		t.Fatal(err)
	default:
	}
}
