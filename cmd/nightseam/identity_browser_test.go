package main

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

	runtime "github.com/Bitspark/nightseam/runtime/go"
)

// The normal full tier compiles the exact browser entry point against the
// checkout's runtime. Executing a browser is separately opt-in: CI does not
// silently acquire a Chromium dependency or substitute Node for a browser.
func TestIdentityBrowserFixtureCompiles(t *testing.T) { buildIdentityBrowser(t) }

func buildIdentityBrowser(t *testing.T) string {
	t.Helper()
	root := repositoryRoot(t)
	tsc := fixture(t, root, "node", "tsc")
	directory := t.TempDir()
	config, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{
			"target": "ES2022", "module": "ESNext", "moduleResolution": "Bundler",
			"strict": true, "skipLibCheck": true, "rewriteRelativeImportExtensions": true,
			"rootDir": root, "outDir": filepath.Join(directory, "site"), "types": []string{},
			"paths": map[string][]string{
				"@nightseam/runtime": {filepath.ToSlash(filepath.Join(root, "runtime/ts/src/index.ts"))},
				"@nightseam/duplex":  {filepath.ToSlash(filepath.Join(root, "duplex/ts/src/index.ts"))},
			},
		},
		"files": []string{filepath.Join(root, "cmd/nightseam/testdata/identity-browser/browser.ts")},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "tsconfig.json", config)
	runFixture(t, directory, "node", tsc, "--project", "tsconfig.json")
	page, err := os.ReadFile(filepath.Join(root, "cmd/nightseam/testdata/identity-browser/index.html"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "site/index.html", page)
	return filepath.Join(directory, "site")
}

type identityBrowserReport struct {
	UserAgent string `json:"userAgent"`
	Rows      []struct {
		Name         string `json:"name"`
		NativeSocket bool   `json:"nativeSocket"`
		Selected     string `json:"selected"`
		Outcome      string `json:"outcome"`
		Message      string `json:"message"`
	} `json:"rows"`
	Failure string `json:"failure"`
}

func TestIdentityBrowserExchange(t *testing.T) {
	if testing.Short() {
		t.Skip("browser execution belongs to the full tier")
	}
	browser := os.Getenv("NIGHTSEAM_BROWSER")
	if browser == "" {
		t.Skip("set NIGHTSEAM_BROWSER to an installed Chromium executable for native-browser evidence; see testdata/identity-browser/README.md")
	}
	if _, err := exec.LookPath(browser); err != nil {
		t.Fatalf("NIGHTSEAM_BROWSER: %v", err)
	}
	site := buildIdentityBrowser(t)
	mux := http.NewServeMux()
	reports := make(chan identityBrowserReport, 1)
	mux.HandleFunc("POST /result", func(w http.ResponseWriter, r *http.Request) {
		var report identityBrowserReport
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&report); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case reports <- report:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "already reported", http.StatusConflict)
		}
	})
	var offeredMu sync.Mutex
	offeredByCase := map[string]string{}
	counts := map[string]*atomic.Int32{}
	for _, protocol := range []string{"none", "ticket"} {
		for _, mode := range []string{"match", "mismatch", "absent", "digestless"} {
			name := mode + "-" + protocol
			calls := &atomic.Int32{}
			counts[name] = calls
			handlers := map[string]runtime.Handler{
				"application.echo": func(_ context.Context, _ *runtime.Peer, raw json.RawMessage) (any, error) {
					calls.Add(1)
					var params struct {
						Value string `json:"value"`
					}
					if err := json.Unmarshal(raw, &params); err != nil {
						return nil, err
					}
					return params.Value, nil
				},
			}
			if mode != "absent" {
				expected := runtime.DeclarationIdentity{Path: "browser.identity", Digest: strings.Repeat("a", 64)}
				if mode == "digestless" {
					expected.Digest = ""
				}
				handler, err := runtime.IdentityHandler(expected)
				if err != nil {
					t.Fatal(err)
				}
				handlers[runtime.IdentityMethod] = handler
			}
			options := runtime.ServerOptions{
				Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
				CheckOrigin:  func(*http.Request) bool { return true },
				Options:      runtime.Options{Handlers: handlers},
			}
			if protocol == "ticket" {
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
	mux.Handle("/", http.FileServer(http.Dir(site)))
	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	profile := filepath.Join(t.TempDir(), "chromium-profile")
	command := exec.CommandContext(ctx, browser, "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--user-data-dir="+profile, server.URL)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- command.Wait() }()
	var report identityBrowserReport
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
	t.Logf("native browser report:\n%s", encoded)
	if report.Failure != "" || len(report.Rows) != len(counts) || !strings.Contains(report.UserAgent, "Chrome/") {
		t.Fatalf("incomplete native browser evidence: %s", encoded)
	}
	seen := map[string]bool{}
	for _, row := range report.Rows {
		counter, exists := counts[row.Name]
		if !exists || seen[row.Name] {
			t.Fatalf("unknown or repeated browser row %q", row.Name)
		}
		seen[row.Name] = true
		protocol := ""
		if strings.HasSuffix(row.Name, "-ticket") {
			protocol = "ticket.fixture"
		}
		offeredMu.Lock()
		offered := offeredByCase[row.Name]
		offeredMu.Unlock()
		if !row.NativeSocket || row.Selected != protocol || offered != protocol {
			t.Errorf("%s: native=%v, offered=%q, selected=%q, want=%q", row.Name, row.NativeSocket, offered, row.Selected, protocol)
		}
		wantCalls := int32(1)
		if strings.HasPrefix(row.Name, "mismatch-") {
			wantCalls = 0
			if row.Outcome != "contract_mismatch" || row.Message != "the declaration identity for browser.identity differs" {
				t.Errorf("%s: mismatch code or message lost", row.Name)
			}
		} else if row.Outcome != "accepted" {
			t.Errorf("%s: not accepted", row.Name)
		}
		if got := counter.Load(); got != wantCalls {
			t.Errorf("%s: application invocations=%d, want=%d", row.Name, got, wantCalls)
		}
	}
}
