package browser_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	binding "example.test/generated/api/go/browser-identity-binding"
	protocol "example.test/generated/api/go/browser-identity-protocol"
	duplex "github.com/Bitspark/nightseam/duplex/go"
	runtime "github.com/Bitspark/nightseam/runtime/go"
)

type browserReport struct {
	UserAgent string `json:"userAgent"`
	Rows      []struct {
		Name         string `json:"name"`
		NativeSocket bool   `json:"nativeSocket"`
		Selected     string `json:"selected"`
		Outcome      string `json:"outcome"`
		Message      string `json:"message"`
		EarlyFrames  int    `json:"earlyFrames"`
		BeforeBind   int    `json:"beforeBind"`
		ReverseCalls int    `json:"reverseCalls"`
		Events       int    `json:"events"`
	} `json:"rows"`
	Failure string `json:"failure"`
}

type caseState struct {
	calls   atomic.Int32
	reverse chan error
}
type application struct{ state *caseState }

func (a application) Echo(_ context.Context, params protocol.Input) (string, error) {
	a.state.calls.Add(1)
	return params.Value, nil
}

func TestNativeBrowserGeneratedIdentity(t *testing.T) {
	browser := os.Getenv("NIGHTSEAM_BROWSER")
	if browser == "" {
		t.Skip("native browser was not requested")
	}
	mux := http.NewServeMux()
	reports := make(chan browserReport, 1)
	mux.HandleFunc("POST /result", func(w http.ResponseWriter, r *http.Request) {
		var report browserReport
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&report); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		select {
		case reports <- report:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "already reported", 409)
		}
	})
	var offeredMu sync.Mutex
	offeredByCase := map[string]string{}
	states := map[string]*caseState{}
	for _, route := range []string{"prepare", "from"} {
		for _, subprotocol := range []string{"none", "ticket"} {
			for _, mode := range []string{"match", "mismatch", "absent", "digestless"} {
				name := route + "-" + mode + "-" + subprotocol
				state := &caseState{reverse: make(chan error, 1)}
				states[name] = state
				var remote protocol.Client
				typed := mode == "match" || mode == "mismatch"
				options := runtime.ServerOptions{
					Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
					CheckOrigin:  func(*http.Request) bool { return true },
				}
				if typed {
					options.Options.Prepare = func(peer *runtime.Peer) error {
						wire, err := binding.ToWire(func(other protocol.Client) (protocol.Server, error) {
							remote = other
							return protocol.Server{Methods: application{state}}, nil
						}, runtime.AdapterContext{})
						if err != nil {
							return err
						}
						detach, err := runtime.ForwardWire(peer.Wire(), wire)
						if err != nil {
							_ = wire.Close(duplex.CodeInternalError, "forwarding failed")
							return err
						}
						go func() { <-peer.Done(); detach(); _ = wire.Close(duplex.CodeNormal, "") }()
						return nil
					}
				} else {
					// Untyped controls expose no identity method, or a path without a digest.
					options.Options.Handlers = map[string]runtime.Handler{
						wireName("echo"): func(ctx context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
							if err := protocol.ValidateRaw("Input", raw); err != nil {
								return nil, err
							}
							var params protocol.Input
							if err := json.Unmarshal(raw, &params); err != nil {
								return nil, err
							}
							return (application{state}).Echo(ctx, params)
						},
					}
					if mode == "digestless" {
						identity, err := runtime.IdentityHandler(runtime.DeclarationIdentity{Path: "browser-identity"})
						if err != nil {
							t.Fatal(err)
						}
						options.Options.Handlers[wireName(runtime.IdentityMethod)] = identity
					}
				}
				if route == "prepare" {
					options.OnConnect = func(peer *runtime.Peer) {
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
							defer cancel()
							params := protocol.Input{Value: name}
							var err error
							if typed {
								err = remote.Events.Ready(ctx, params)
							} else {
								err = peer.Emit(ctx, wireName("ready"), params)
							}
							if err != nil {
								state.reverse <- err
								return
							}
							var result string
							if typed {
								result, err = remote.Methods.Reverse(ctx, params)
							} else {
								err = peer.Call(ctx, wireName("reverse"), params, &result)
							}
							if err == nil && result != name {
								err = &runtime.PublicError{Code: "wrong_result", Message: result}
							}
							state.reverse <- err
						}()
					}
				}
				if subprotocol == "ticket" {
					options.Subprotocols = []string{"ticket.fixture"}
				}
				handler, err := runtime.NewHandler(options)
				if err != nil {
					t.Fatal(err)
				}
				mux.HandleFunc("/socket/"+name, func(w http.ResponseWriter, r *http.Request) {
					offeredMu.Lock()
					offeredByCase[name] = r.Header.Get("Sec-WebSocket-Protocol")
					offeredMu.Unlock()
					handler.ServeHTTP(w, r)
				})
			}
		}
	}
	mux.Handle("/", http.FileServer(http.Dir("site")))
	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, browser, "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--user-data-dir="+filepath.Join(t.TempDir(), "chromium-profile"), server.URL)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- command.Wait() }()
	var report browserReport
	select {
	case report = <-reports:
		cancel()
		<-stopped
	case err := <-stopped:
		t.Fatalf("browser exited before reporting: %v\n%s", err, output.String())
	case <-ctx.Done():
		<-stopped
		t.Fatalf("browser never reported: %v\n%s", ctx.Err(), output.String())
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	t.Logf("native generated browser report:\n%s", encoded)
	if report.Failure != "" || len(report.Rows) != len(states) || !strings.Contains(report.UserAgent, "Chrome/") {
		t.Fatalf("incomplete native browser evidence: %s", encoded)
	}
	seen := map[string]bool{}
	for _, row := range report.Rows {
		state, exists := states[row.Name]
		if !exists || seen[row.Name] {
			t.Fatalf("unknown or repeated row %q", row.Name)
		}
		seen[row.Name] = true
		selected := ""
		if strings.HasSuffix(row.Name, "-ticket") {
			selected = "ticket.fixture"
		}
		offeredMu.Lock()
		offered := offeredByCase[row.Name]
		offeredMu.Unlock()
		if !row.NativeSocket || row.Selected != selected || offered != selected {
			t.Errorf("%s: native=%v, offered=%q, selected=%q, want=%q", row.Name, row.NativeSocket, offered, row.Selected, selected)
		}
		mismatch := strings.Contains(row.Name, "-mismatch-")
		wantCalls := int32(1)
		if mismatch {
			wantCalls = 0
			if row.Outcome != "contract_mismatch" || row.Message != "the declaration identity for browser-identity differs" {
				t.Errorf("%s lost mismatch code/message", row.Name)
			}
		} else if row.Outcome != "accepted" {
			t.Errorf("%s was refused", row.Name)
		}
		if got := state.calls.Load(); got != wantCalls {
			t.Errorf("%s: application invocations=%d, want=%d", row.Name, got, wantCalls)
		}
		if row.BeforeBind != 0 {
			t.Errorf("%s: application callback ran before factory binding", row.Name)
		}
		wantEarly, wantCallbacks := 0, 0
		if strings.HasPrefix(row.Name, "prepare-") {
			wantEarly = 2
			if !mismatch {
				wantCallbacks = 1
			}
			select {
			case err := <-state.reverse:
				if (err == nil) == mismatch {
					t.Errorf("%s: reverse completion=%v", row.Name, err)
				}
			case <-time.After(3 * time.Second):
				t.Errorf("%s: reverse call never completed", row.Name)
			}
		}
		if row.EarlyFrames != wantEarly || row.ReverseCalls != wantCallbacks || row.Events != wantCallbacks {
			t.Errorf("%s: early=%d reverse=%d events=%d, want=%d/%d/%d", row.Name, row.EarlyFrames, row.ReverseCalls, row.Events, wantEarly, wantCallbacks, wantCallbacks)
		}
		t.Logf("verified %s: Go application dispatch=%d, native early frames=%d, bound reverse/events=%d/%d", row.Name, state.calls.Load(), row.EarlyFrames, row.ReverseCalls, row.Events)
	}
}

func wireName(name string) string {
	encoded, err := duplex.EncodePath([]string{name})
	if err != nil {
		panic(err)
	}
	return encoded
}
