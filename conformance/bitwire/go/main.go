// Adapted from Bitspark/bitwire v0.1.0 (9f45a2e0), conformance/drivers/nightseam/go/main.go.
// Apache-2.0; Bitspark. Expected observations remain in the published Bitwire module.
// It does not contain a replacement Wire implementation or expected results.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"time"

	bitwire "github.com/Bitspark/bitwire/wire/go"
	"github.com/Bitspark/nightseam/duplex/go"
	ns "github.com/Bitspark/nightseam/runtime/go"
)

type registration struct {
	ID        string   `json:"id"`
	Path      []string `json:"path"`
	Namespace bool     `json:"namespace"`
	Result    any      `json:"result"`
}
type testCase struct {
	ID              string                        `json:"id"`
	Kind            string                        `json:"kind"`
	Prefix          []string                      `json:"prefix"`
	Selections      [][]string                    `json:"selections"`
	MountKey        *string                       `json:"mountKey"`
	Path            []string                      `json:"path"`
	Payload         any                           `json:"payload"`
	Registrations   []registration                `json:"registrations"`
	Calls           []struct{ Path []string }     `json:"calls"`
	Duplicate       struct{ Registration string } `json:"duplicate"`
	ProfileRefusals [][]string                    `json:"profileRefusals"`
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

var cleanups []func()

func pair() (bitwire.Wire, bitwire.Wire) {
	if os.Getenv("NIGHTSEAM_BITWIRE_CARRIER") == "local" {
		a, b, err := ns.NewWirePair(ns.Options{})
		check(err)
		return a, b
	}
	connected := make(chan *ns.Peer, 1)
	handler, err := ns.NewHandler(ns.ServerOptions{
		Authenticate: func(r *http.Request) (context.Context, error) { return r.Context(), nil },
		CheckOrigin:  func(*http.Request) bool { return true },
		OnConnect:    func(peer *ns.Peer) { connected <- peer },
	})
	check(err)
	server := httptest.NewServer(handler)
	cleanups = append(cleanups, server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	cleanups = append(cleanups, cancel)
	client, _, err := ns.Dial(ctx, server.URL, ns.DialOptions{ConnectTimeout: 5 * time.Second})
	check(err)
	cleanups = append(cleanups, func() { _ = client.Close() })
	var remote *ns.Peer
	select {
	case remote = <-connected:
	case <-time.After(5 * time.Second):
		panic("the server did not publish its accepted peer")
	case <-ctx.Done():
		panic(ctx.Err())
	}
	cleanups = append(cleanups, func() { _ = remote.Close() })
	if os.Getenv("NIGHTSEAM_BITWIRE_REVERSE") == "1" {
		return remote.Wire(), client.Wire()
	}
	return client.Wire(), remote.Wire()
}
func call(wire bitwire.Wire, path []string, value any) any {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result any
	check(ns.CallWire(ctx, wire, path, value, &result))
	return result
}
func reply(message bitwire.Message, value any) {
	if message.Frame.Kind != bitwire.ProfileRequest || message.Return == nil {
		panic("expected a request with return access")
	}
	encoded, err := json.Marshal(value)
	check(err)
	check(message.Return.Wire.Send(nil, bitwire.Message{Frame: bitwire.ProfileFrame{
		Version: 1, Kind: bitwire.ProfileResponse, ID: message.Frame.ID, Result: encoded,
	}}))
}
func echo(_ []string, message bitwire.Message) { reply(message, message.Frame.Params) }
func closeWire(wire bitwire.Wire)              { check(wire.Close(duplex.CodeNormal, "conformance complete")) }
func event() bitwire.Message {
	return bitwire.Message{Frame: bitwire.ProfileFrame{Version: 1, Kind: bitwire.ProfileEvent, Data: json.RawMessage("null")}}
}
func copyPath(path []string) []string { return append([]string{}, path...) }

// Every operation is delegated. The spies observe the boundary before/after
// composition, without implementing routing, dispatch or return correlation.
type observer struct {
	bitwire.Wire
	send    func([]string, bitwire.Message)
	receive func([]string, bitwire.Message)
}

func (w observer) Send(path []string, message bitwire.Message) error {
	if w.send != nil {
		w.send(path, message)
	}
	return w.Wire.Send(path, message)
}
func (w observer) Receive(path []string, receiver bitwire.Receiver) (func(), error) {
	return w.Wire.Receive(path, bitwire.Receiver{Namespace: receiver.Namespace, Closed: receiver.Closed,
		Message: func(path []string, message bitwire.Message) {
			if w.receive != nil {
				w.receive(path, message)
			}
			if receiver.Message != nil {
				receiver.Message(path, message)
			}
		},
	})
}

func access(test testCase) any {
	client, server := pair()
	defer closeWire(client)
	var originalReturn, rootReturn, admittedReturn, receivedReturn *bitwire.ReturnAddress
	var rootPath, receiverPath []string
	clientSpy := observer{Wire: client, send: func(path []string, m bitwire.Message) {
		rootPath, rootReturn = copyPath(path), m.Return
	}}
	serverSpy := observer{Wire: server, receive: func(_ []string, m bitwire.Message) { admittedReturn = m.Return }}
	selected := duplex.At(clientSpy, test.Prefix)
	if test.MountKey != nil {
		mounted := duplex.Mount(map[string]bitwire.Wire{*test.MountKey: selected})
		defer closeWire(mounted)
		selected = duplex.At(mounted, []string{*test.MountKey})
	}
	prefix := copyPath(test.Prefix)
	for _, part := range test.Selections {
		selected = duplex.At(selected, part)
		prefix = append(prefix, part...)
	}
	outer := observer{Wire: selected, send: func(_ []string, m bitwire.Message) { originalReturn = m.Return }}
	receiver := duplex.At(serverSpy, prefix)
	_, err := receiver.Receive(test.Path, bitwire.Receiver{Message: func(path []string, message bitwire.Message) {
		receiverPath, receivedReturn = copyPath(path), message.Return
		echo(path, message)
	}})
	check(err)
	value := call(outer, test.Path, test.Payload)
	return map[string]any{"rootPath": rootPath, "receiverPath": receiverPath, "payload": value,
		"sendReturnIdentity":    originalReturn != nil && originalReturn == rootReturn,
		"receiveReturnIdentity": admittedReturn != nil && admittedReturn == receivedReturn}
}

func routing(test testCase) any {
	client, server := pair()
	defer closeWire(client)
	var duplicate registration
	for _, entry := range test.Registrations {
		if entry.ID == test.Duplicate.Registration {
			duplicate = entry
		}
		_, err := server.Receive(entry.Path, bitwire.Receiver{Namespace: entry.Namespace, Message: func(path []string, message bitwire.Message) {
			reply(message, map[string]any{"result": entry.Result, "receiverPath": copyPath(path), "receiver": entry.ID})
		}})
		check(err)
	}
	if duplicate.ID == "" {
		panic("duplicate target does not name a registration")
	}
	detach, err := server.Receive(duplicate.Path, bitwire.Receiver{Namespace: duplicate.Namespace, Message: echo})
	refused := err != nil
	if detach != nil {
		detach()
	}
	results := []any{}
	for _, entry := range test.Calls {
		results = append(results, call(client, entry.Path, nil))
	}
	observations := map[string]any{"calls": results, "duplicateRefused": refused}
	if test.ProfileRefusals != nil {
		refusals := []any{}
		for _, path := range test.ProfileRefusals {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			var result any
			err := ns.CallWire(ctx, client, path, nil, &result)
			cancel()
			var unpublished *ns.UnpublishedError
			refusals = append(refusals, map[string]any{"path": copyPath(path), "refused": errors.As(err, &unpublished)})
		}
		observations["requestRefusals"] = refusals
	}
	return observations
}

func lifetime(test testCase) any {
	client, server := pair()
	defer closeWire(client)
	mounted := duplex.Mount(map[string]bitwire.Wire{"leaf": client})
	defer closeWire(mounted)
	var mountCloseCount, detachCloseCount atomic.Int32
	detach, err := server.Receive(test.Path, bitwire.Receiver{Message: echo, Closed: func(duplex.Code, string) { detachCloseCount.Add(1) }})
	check(err)
	selected := duplex.At(mounted, []string{"leaf"})
	before := call(selected, test.Path, test.Payload)
	detach()
	detach()
	detachAgain, err := server.Receive(test.Path, bitwire.Receiver{Message: echo})
	check(err)
	afterDetach := call(selected, test.Path, test.Payload)
	_, err = mounted.Receive(nil, bitwire.Receiver{Namespace: true, Message: func([]string, bitwire.Message) {}, Closed: func(duplex.Code, string) { mountCloseCount.Add(1) }})
	check(err)
	mountOriginRefused := mounted.Send(nil, event()) != nil
	closeWire(mounted)
	closeWire(mounted)
	// The former namespace must be free again on the borrowed endpoint.
	detachNamespace, err := client.Receive(nil, bitwire.Receiver{Namespace: true, Message: func([]string, bitwire.Message) {}})
	mountRegistrationReleased := err == nil
	if detachNamespace != nil {
		detachNamespace()
	}
	afterMountClose := call(client, test.Path, test.Payload)
	detachAgain()
	ended := make(chan struct{})
	_, err = server.Receive(test.Path, bitwire.Receiver{Message: echo, Closed: func(duplex.Code, string) { close(ended) }})
	check(err)
	closeWire(duplex.At(client, nil))
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		panic("selected close did not notify endpoint receiver")
	}
	selectedCloseRefused := client.Send(test.Path, event()) != nil
	return map[string]any{"before": before, "afterDetach": afterDetach, "afterMountClose": afterMountClose,
		"selectedCloseRefused": selectedCloseRefused, "mountOriginRefused": mountOriginRefused,
		"mountCloseCount": mountCloseCount.Load(), "detachCloseCount": detachCloseCount.Load(),
		"mountRegistrationReleased": mountRegistrationReleased}
}

func forwarding(test testCase) any {
	caller, inbound := pair()
	outbound, server := pair()
	defer closeWire(caller)
	defer closeWire(outbound)
	var inboundReturn, outboundReturn *bitwire.ReturnAddress
	a := observer{Wire: inbound, receive: func(_ []string, m bitwire.Message) { inboundReturn = m.Return }}
	b := observer{Wire: outbound, send: func(_ []string, m bitwire.Message) { outboundReturn = m.Return }}
	detach, err := ns.ForwardWire(a, b)
	check(err)
	defer detach()
	_, err = server.Receive(test.Path, bitwire.Receiver{Message: echo})
	check(err)
	forwarded := call(caller, test.Path, test.Payload)
	identity := inboundReturn != nil && inboundReturn == outboundReturn
	detach()
	detach()
	_, err = inbound.Receive(test.Path, bitwire.Receiver{Message: echo})
	check(err)
	inboundAfterDetach := call(caller, test.Path, test.Payload)
	outboundAfterDetach := call(outbound, test.Path, test.Payload)
	return map[string]any{"forwarded": forwarded, "forwardReturnIdentity": identity,
		"inboundAfterDetach": inboundAfterDetach, "outboundAfterDetach": outboundAfterDetach}
}

func main() {
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}()
	if carrier := os.Getenv("NIGHTSEAM_BITWIRE_CARRIER"); carrier != "local" && carrier != "peer" {
		panic("expected local or peer carrier")
	}
	if len(os.Args) != 2 {
		panic("usage: driver <cases.json>")
	}
	data, err := os.ReadFile(os.Args[1])
	check(err)
	var fixture struct {
		SchemaVersion int        `json:"schemaVersion"`
		Cases         []testCase `json:"cases"`
	}
	check(json.Unmarshal(data, &fixture))
	if fixture.SchemaVersion != 1 || len(fixture.Cases) == 0 {
		panic("unsupported or empty fixture")
	}
	results := []any{}
	for _, test := range fixture.Cases {
		if os.Getenv("NIGHTSEAM_BITWIRE_CARRIER") == "peer" && test.ID == "distinct-opaque-paths" {
			fmt.Fprintln(os.Stderr, "Explicit physical applicability exclusion: exact [] registration unsupported by peer profile")
			continue
		}
		var result any
		switch test.Kind {
		case "access":
			result = access(test)
		case "routing":
			result = routing(test)
		case "lifetime":
			result = lifetime(test)
		case "forwarding":
			result = forwarding(test)
		default:
			panic(fmt.Sprintf("unknown case kind %q", test.Kind))
		}
		results = append(results, map[string]any{"id": test.ID, "observations": result})
	}
	check(json.NewEncoder(os.Stdout).Encode(results))
}
