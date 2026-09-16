package wsruntime_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ws "github.com/Bitspark/nighthall/api/go/ws-runtime"
	"github.com/coder/websocket"
)

func receive[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for peer activity")
		var zero T
		return zero
	}
}

func newPair(t *testing.T, serverOptions, clientOptions ws.Options) (*ws.Peer, *ws.Peer) {
	t.Helper()
	connected := make(chan *ws.Peer, 1)
	handler, err := ws.NewHandler(ws.ServerOptions{
		Options:      serverOptions,
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
		OnConnect:    func(peer *ws.Peer) { connected <- peer },
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client, _, err := ws.Dial(ctx, server.URL, ws.DialOptions{Options: clientOptions})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	remote := receive(t, connected)
	t.Cleanup(func() { _ = remote.Close() })
	return client, remote
}

func TestReverseCallCompletesWhileOriginalRequestIsOutstanding(t *testing.T) {
	client, _ := newPair(t, ws.Options{Handlers: map[string]ws.Handler{
		"outer": func(ctx context.Context, peer *ws.Peer, _ json.RawMessage) (any, error) {
			var response int
			if err := peer.Call(ctx, "reverse", 6, &response); err != nil {
				return nil, err
			}
			return response + 1, nil
		},
		"inner": func(_ context.Context, _ *ws.Peer, data json.RawMessage) (any, error) {
			var value int
			if err := json.Unmarshal(data, &value); err != nil {
				return nil, err
			}
			return value * 7, nil
		},
	}}, ws.Options{Handlers: map[string]ws.Handler{
		"reverse": func(ctx context.Context, peer *ws.Peer, data json.RawMessage) (any, error) {
			var response int
			err := peer.Call(ctx, "inner", data, &response)
			return response, err
		},
	}})
	var result int
	if err := client.Call(context.Background(), "outer", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result != 43 {
		t.Fatalf("nested duplex result = %d, want 43", result)
	}
}

func TestEventsTravelInBothDirections(t *testing.T) {
	clientEvents, serverEvents := make(chan string, 2), make(chan string, 2)
	eventHandler := func(output chan<- string) ws.EventHandler {
		return func(_ context.Context, _ *ws.Peer, data json.RawMessage) { output <- string(data) }
	}
	client, server := newPair(t,
		ws.Options{Events: map[string]ws.EventHandler{"progress": eventHandler(serverEvents)}},
		ws.Options{Events: map[string]ws.EventHandler{"progress": eventHandler(clientEvents)}})
	observed := make(chan ws.Event, 1)
	unsubscribe := client.OnEvent(func(_ context.Context, event ws.Event) { observed <- event })
	for _, value := range []int{1, 2} {
		if err := client.Emit(context.Background(), "progress", value); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.Emit(context.Background(), "progress", "done"); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, serverEvents); got != "1" {
		t.Fatalf("first server event = %s", got)
	}
	if got := receive(t, serverEvents); got != "2" {
		t.Fatalf("second server event = %s", got)
	}
	if got := receive(t, clientEvents); got != `"done"` {
		t.Fatalf("client event = %s", got)
	}
	if got := receive(t, observed); got.Name != "progress" || string(got.Data) != `"done"` {
		t.Fatalf("observed event = %+v", got)
	}
	unsubscribe()
	unsubscribe()
}

func TestPublicErrorsArePreservedAndInternalFailuresHidden(t *testing.T) {
	client, _ := newPair(t, ws.Options{Handlers: map[string]ws.Handler{
		"public": func(context.Context, *ws.Peer, json.RawMessage) (any, error) {
			return nil, fmt.Errorf("wrapper: %w", &ws.PublicError{Code: "conflict", Message: "Changed", Data: json.RawMessage(`{"revision":3}`)})
		},
		"private": func(context.Context, *ws.Peer, json.RawMessage) (any, error) {
			return nil, errors.New("private database password")
		},
		"panic": func(context.Context, *ws.Peer, json.RawMessage) (any, error) {
			panic("private panic details")
		},
		"unencodable": func(context.Context, *ws.Peer, json.RawMessage) (any, error) {
			return make(chan int), nil
		},
		"ok": func(context.Context, *ws.Peer, json.RawMessage) (any, error) { return true, nil },
	}}, ws.Options{})
	for _, test := range []struct{ method, code, message string }{
		{"public", "conflict", "Changed"},
		{"private", "internal", "Internal error"},
		{"panic", "internal", "Internal error"},
		{"unencodable", "internal", "Internal error"},
		{"missing", "method_not_found", "Unknown method"},
	} {
		t.Run(test.method, func(t *testing.T) {
			err := client.Call(context.Background(), test.method, nil, nil)
			var public *ws.PublicError
			if !errors.As(err, &public) || public.Code != test.code || public.Message != test.message {
				t.Fatalf("error = %v, want %s: %s", err, test.code, test.message)
			}
			if test.method == "public" && string(public.Data) != `{"revision":3}` {
				t.Fatalf("public error data = %s", public.Data)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatalf("internal error leaked: %v", err)
			}
		})
	}
	var result bool
	if err := client.Call(context.Background(), "ok", nil, &result); err != nil || !result {
		t.Fatalf("connection did not survive handler failure: result=%v err=%v", result, err)
	}
}

func TestCallerCancellationReachesRemoteHandler(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan error, 1)
	client, server := newPair(t, ws.Options{Handlers: map[string]ws.Handler{
		"wait": func(ctx context.Context, _ *ws.Peer, _ json.RawMessage) (any, error) {
			close(started)
			<-ctx.Done()
			cancelled <- ctx.Err()
			return nil, ctx.Err()
		},
	}}, ws.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	returned := make(chan error, 1)
	go func() { returned <- client.Call(ctx, "wait", nil, nil) }()
	receive(t, started)
	cancel()
	if err := receive(t, returned); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller result = %v", err)
	}
	if err := receive(t, cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("handler context = %v", err)
	}
	if client.Err() != nil || server.Err() != nil {
		t.Fatalf("request cancellation closed connection: client=%v server=%v", client.Err(), server.Err())
	}
}

func TestSaturationRejectsNewWorkButStillRoutesReverseResponses(t *testing.T) {
	reverseStarted, release := make(chan struct{}), make(chan struct{})
	client, _ := newPair(t, ws.Options{
		MaxConcurrentHandlers: 1,
		Handlers: map[string]ws.Handler{
			"outer": func(ctx context.Context, peer *ws.Peer, _ json.RawMessage) (any, error) {
				var result string
				err := peer.Call(ctx, "reverse", nil, &result)
				return result, err
			},
			"extra": func(context.Context, *ws.Peer, json.RawMessage) (any, error) { return "unexpected", nil },
		},
	}, ws.Options{Handlers: map[string]ws.Handler{
		"reverse": func(ctx context.Context, _ *ws.Peer, _ json.RawMessage) (any, error) {
			close(reverseStarted)
			select {
			case <-release:
				return "released", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}})
	type callResult struct {
		value string
		err   error
	}
	returned := make(chan callResult, 1)
	go func() {
		var value string
		err := client.Call(context.Background(), "outer", nil, &value)
		returned <- callResult{value, err}
	}()
	receive(t, reverseStarted)
	err := client.Call(context.Background(), "extra", nil, nil)
	var public *ws.PublicError
	if !errors.As(err, &public) || public.Code != "busy" {
		t.Fatalf("saturated request returned %v, want busy", err)
	}
	close(release)
	if got := receive(t, returned); got.err != nil || got.value != "released" {
		t.Fatalf("pending reverse response was blocked by handler saturation: %+v", got)
	}
}

func TestStalledEventConsumerDisconnects(t *testing.T) {
	started := make(chan struct{})
	client, server := newPair(t, ws.Options{}, ws.Options{
		QueueCapacity: 1,
		Events: map[string]ws.EventHandler{
			"progress": func(ctx context.Context, _ *ws.Peer, _ json.RawMessage) {
				close(started)
				<-ctx.Done()
			},
		},
	})
	if err := server.Emit(context.Background(), "progress", 1); err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	for _, value := range []int{2, 3} {
		if err := server.Emit(context.Background(), "progress", value); err != nil {
			t.Fatal(err)
		}
	}
	receive(t, client.Done())
	if !errors.Is(client.Err(), ws.ErrBackpressure) {
		t.Fatalf("stalled event consumer error = %v", client.Err())
	}
	receive(t, server.Done())
}

func TestDisconnectCancelsHandlersAndRejectsPendingCalls(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	client, server := newPair(t, ws.Options{Handlers: map[string]ws.Handler{
		"wait": func(ctx context.Context, _ *ws.Peer, _ json.RawMessage) (any, error) {
			close(started)
			<-ctx.Done()
			close(stopped)
			return nil, ctx.Err()
		},
	}}, ws.Options{})
	returned := make(chan error, 1)
	go func() { returned <- client.Call(context.Background(), "wait", nil, nil) }()
	receive(t, started)
	_ = server.Close()
	receive(t, stopped)
	if err := receive(t, returned); err == nil {
		t.Fatal("pending call succeeded after disconnect")
	}
	receive(t, client.Done())
}

func TestServerRequiresAndEnforcesAuthenticationAndOriginPolicies(t *testing.T) {
	authenticate := func(r *http.Request) (context.Context, error) { return r.Context(), nil }
	allowOrigin := func(*http.Request) bool { return true }
	for _, options := range []ws.ServerOptions{{}, {Authenticate: authenticate}, {CheckOrigin: allowOrigin}} {
		if _, err := ws.NewHandler(options); err == nil {
			t.Fatal("server accepted missing explicit policy")
		}
	}
	type userKey struct{}
	handler, err := ws.NewHandler(ws.ServerOptions{
		Authenticate: func(r *http.Request) (context.Context, error) {
			if r.Header.Get("Authorization") != "Bearer valid" {
				return nil, errors.New("private authentication failure")
			}
			return context.WithValue(r.Context(), userKey{}, "alice"), nil
		},
		CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "https://allowed.example" },
		Options: ws.Options{Handlers: map[string]ws.Handler{
			"identity": func(ctx context.Context, _ *ws.Peer, _ json.RawMessage) (any, error) {
				return ctx.Value(userKey{}), nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, test := range []struct {
		origin, authorization string
		status                int
	}{
		{"https://denied.example", "Bearer valid", http.StatusForbidden},
		{"https://allowed.example", "Bearer wrong", http.StatusUnauthorized},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		peer, response, err := ws.Dial(ctx, server.URL, ws.DialOptions{HTTPHeader: http.Header{
			"Origin": {test.origin}, "Authorization": {test.authorization},
		}})
		cancel()
		if peer != nil {
			_ = peer.Close()
		}
		if err == nil || response == nil || response.StatusCode != test.status {
			t.Fatalf("rejected handshake = peer %v, response %v, error %v; want %d", peer, response, err, test.status)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, _, err := ws.Dial(ctx, server.URL, ws.DialOptions{HTTPHeader: http.Header{
		"Origin": {"https://allowed.example"}, "Authorization": {"Bearer valid"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var identity string
	if err := client.Call(ctx, "identity", nil, &identity); err != nil || identity != "alice" {
		t.Fatalf("authenticated identity = %q, error=%v", identity, err)
	}
}

func TestMalformedWireFramesDisconnect(t *testing.T) {
	for _, data := range []string{
		`{"version":2,"kind":"event","event":"progress","data":1}`,
		`{"version":1,"kind":"request","id":"s:1","method":"wait","params":null}`,
		`{"version":1,"kind":"response","id":"s:1","result":null,"error":{"code":"bad","message":"bad"}}`,
		`{"version":1,"kind":"event","event":"progress","data":1,"extra":true}`,
	} {
		t.Run(data, func(t *testing.T) {
			connected := make(chan *ws.Peer, 1)
			handler, err := ws.NewHandler(ws.ServerOptions{
				Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
				CheckOrigin:  func(*http.Request) bool { return true },
				OnConnect:    func(peer *ws.Peer) { connected <- peer },
			})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			peer := receive(t, connected)
			if err := conn.Write(ctx, websocket.MessageText, []byte(data)); err != nil {
				t.Fatal(err)
			}
			receive(t, peer.Done())
			if peer.Err() == nil {
				t.Fatal("malformed frame closed without error")
			}
		})
	}
}
