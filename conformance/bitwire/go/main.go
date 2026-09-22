// Exercises actual Nightseam carriers against Bitwire v0.2.0 composition observations.
// Apache-2.0; Bitspark. Expected observations remain in the published Bitwire module.
// It does not contain a replacement Wire implementation or expected results.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"strings"
	"time"

	bitwire "github.com/Bitspark/bitwire/wire/go"
	"github.com/Bitspark/nightseam/duplex/go"
	ns "github.com/Bitspark/nightseam/runtime/go"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}

var cleanups []func()

func pair() (bitwire.Endpoint, bitwire.Endpoint) {
	if os.Getenv("NIGHTSEAM_BITWIRE_CARRIER") == "local" {
		a, b, err := ns.NewWirePair(ns.Options{})
		check(err)
		cleanups = append(cleanups, func() { closeWire(a); closeWire(b) })
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
func closeWire(wire interface {
	Close(bitwire.Code, string) error
}) {
	check(wire.Close(duplex.CodeNormal, "conformance complete"))
}
func event() bitwire.Message {
	return bitwire.Message{Frame: bitwire.ProfileFrame{Version: 1, Kind: bitwire.ProfileEvent, Data: json.RawMessage("null")}}
}
func copyPath(path []string) []string { return append([]string{}, path...) }

// Every operation is delegated. The spies observe the boundary before/after
// composition, without implementing routing, dispatch or return correlation.
type observer struct {
	bitwire.Endpoint
	send    func([]string, bitwire.Message)
	receive func([]string, bitwire.Message)
}

func (w observer) Send(path []string, message bitwire.Message) error {
	if w.send != nil {
		w.send(path, message)
	}
	return w.Endpoint.Send(path, message)
}
func (w observer) Receive(receiver bitwire.Receiver) (func(), error) {
	return w.Endpoint.Receive(bitwire.Receiver{Closed: receiver.Closed,
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

// These helpers provide only deadlines and observations. The production pair,
// peer, dispatcher, selected endpoints, mount and forwarder do all delivery.
func wait[T any](ch <-chan T) T {
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		panic("delivery deadline exceeded")
	}
}
func attach(e bitwire.Endpoint, r bitwire.Receiver) func() {
	off, err := e.Receive(r)
	check(err)
	return off
}
func dispatcher(e bitwire.Endpoint) *ns.Dispatcher {
	d, err := ns.NewDispatcher(e)
	check(err)
	cleanups = append(cleanups, func() { closeWire(d) })
	return d
}
func send(w bitwire.Wire, path []string, done <-chan struct{}) {
	check(w.Send(path, event()))
	wait(done)
}
func goroutine() string {
	var data [128]byte
	n := runtime.Stack(data[:], false)
	return strings.Fields(string(data[:n]))[1]
}
func siblings() any {
	client, server := pair()
	router := dispatcher(server)
	deliveries := []string{}
	done := make(chan struct{}, 4)
	sender := goroutine()
	receiverRanDuringSend := false
	receiver := func(name string) bitwire.Receiver {
		return bitwire.Receiver{Message: func(path []string, _ bitwire.Message) {
			receiverRanDuringSend = receiverRanDuringSend || goroutine() == sender
			deliveries = append(deliveries, name+":"+strings.Join(path, "/"))
			done <- struct{}{}
		}}
	}
	detachA := attach(router.Select([]string{"a"}), receiver("a"))
	attach(router.Select([]string{"b"}), receiver("b"))
	_, err := server.Receive(bitwire.Receiver{})
	duplicateEndpointAttachmentRefused := err != nil
	send(duplex.At(client, []string{"a"}), []string{"run"}, done)
	send(duplex.At(client, []string{"b"}), []string{"run"}, done)
	detachA()
	detachA()
	check(duplex.At(client, []string{"a"}).Send([]string{"ignored"}, event()))
	send(duplex.At(client, []string{"b"}), []string{"after-detach"}, done)
	closeWire(router)
	attachmentReusableAfterDetach := false
	replacement := attach(server, bitwire.Receiver{Message: func([]string, bitwire.Message) { attachmentReusableAfterDetach = true; done <- struct{}{} }})
	closeWire(router)
	send(client, []string{"replacement"}, done)
	replacement()
	return map[string]any{"deliveries": deliveries, "receiverRanDuringSend": receiverRanDuringSend, "duplicateEndpointAttachmentRefused": duplicateEndpointAttachmentRefused, "attachmentReusableAfterDetach": attachmentReusableAfterDetach}
}
func overlap() any {
	client, server := pair()
	router := dispatcher(server)
	deliveries := []string{}
	done := make(chan struct{}, 2)
	attach(router.Select([]string{"a"}), bitwire.Receiver{Message: func(path []string, _ bitwire.Message) {
		deliveries = append(deliveries, "parent:"+strings.Join(path, "/"))
		done <- struct{}{}
	}})
	detach := attach(router.Select([]string{"a", "b"}), bitwire.Receiver{Message: func(path []string, _ bitwire.Message) {
		deliveries = append(deliveries, "deep:"+strings.Join(path, "/"))
		done <- struct{}{}
	}})
	_, err := router.Select([]string{"a"}).Receive(bitwire.Receiver{})
	duplicateRouteRefused := err != nil
	send(client, []string{"a", "b", "run"}, done)
	detach()
	send(client, []string{"a", "b", "run"}, done)
	return map[string]any{"deliveries": deliveries, "duplicateRouteRefused": duplicateRouteRefused}
}
func composition() any {
	caller, inbound := pair()
	outbound, destination := pair()
	var entered, arrived bitwire.Message
	framePreserved, returnIdentityPreserved, associatedContextPreserved := true, true, false
	marker := new(int)
	associated := map[*bitwire.ReturnAddress]*int{}
	left := observer{Endpoint: inbound, receive: func(_ []string, m bitwire.Message) { entered = m }}
	right := observer{Endpoint: outbound, send: func(_ []string, m bitwire.Message) {
		framePreserved = framePreserved && reflect.DeepEqual(m.Frame, entered.Frame)
		returnIdentityPreserved = returnIdentityPreserved && m.Return != nil && m.Return == entered.Return
	}}
	detach, err := ns.ForwardWire(left, right)
	check(err)
	defer detach()
	router := dispatcher(observer{Endpoint: destination, receive: func(_ []string, m bitwire.Message) { arrived = m; associated[m.Return] = marker }})
	view := router.Select([]string{"a"}).Select([]string{"b"})
	captured := make(chan bitwire.Message, 1)
	deliveredPath := []string{}
	stop := attach(view, bitwire.Receiver{Message: func(path []string, m bitwire.Message) {
		deliveredPath = copyPath(path)
		framePreserved = framePreserved && reflect.DeepEqual(m.Frame, arrived.Frame)
		returnIdentityPreserved = returnIdentityPreserved && m.Return != nil && m.Return == arrived.Return
		associatedContextPreserved = m.Return != nil && associated[m.Return] == marker
		captured <- m
	}})
	result := make(chan any, 1)
	original := bitwire.Message{Frame: bitwire.ProfileFrame{Version: 1, Kind: bitwire.ProfileRequest, ID: "c:1", Params: json.RawMessage(`{"nested":[null,42,"value"]}`), Traceparent: "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01", Tracestate: "vendor=opaque", Meta: map[string]string{"key": "value"}}, Return: &bitwire.ReturnAddress{Wire: replySink(func(_ []string, m bitwire.Message) error {
		if m.Frame.Kind != bitwire.ProfileResponse || m.Frame.Error != nil {
			panic("expected successful reply")
		}
		var value any
		check(json.Unmarshal(m.Frame.Result, &value))
		result <- value
		return nil
	})}}
	callerRouter := dispatcher(observer{Endpoint: caller, send: func(_ []string, m bitwire.Message) {
		framePreserved = framePreserved && reflect.DeepEqual(m.Frame, original.Frame)
		returnIdentityPreserved = returnIdentityPreserved && m.Return == original.Return
	}})
	mounted := duplex.Mount(map[string]bitwire.Endpoint{"": callerRouter.Select([]string{"a"}).Select([]string{"b"})})
	defer closeWire(mounted)
	composed := duplex.At(mounted, []string{""})
	if _, ok := composed.(bitwire.Endpoint); ok {
		panic("selected access grants ownership")
	}
	check(composed.Send([]string{"run", ""}, original))
	request := wait(captured)
	detach()
	detach()
	stop()
	closeWire(view)
	closeWire(mounted)
	closeWire(callerRouter)
	reply(request, "answer")
	answer := wait(result)
	done := make(chan struct{}, 2)
	sourceStillUsable, targetStillUsable := false, false
	stopSource := attach(inbound, bitwire.Receiver{Message: func([]string, bitwire.Message) { sourceStillUsable = true; done <- struct{}{} }})
	send(caller, []string{"probe"}, done)
	stopSource()
	attach(router.Select([]string{"probe"}), bitwire.Receiver{Message: func([]string, bitwire.Message) { targetStillUsable = true; done <- struct{}{} }})
	send(outbound, []string{"probe"}, done)
	return map[string]any{"deliveredPath": deliveredPath, "framePreserved": framePreserved, "returnIdentityPreserved": returnIdentityPreserved, "associatedContextPreserved": associatedContextPreserved, "reply": answer, "borrowedEndpointUsableAfterDetach": sourceStillUsable && targetStillUsable}
}
func opaquePaths() any {
	client, server := pair()
	router := dispatcher(server)
	result := []string{}
	for _, entry := range []struct {
		path []string
		name string
	}{{[]string{""}, "empty"}, {[]string{"a/b"}, "slash"}, {[]string{"a", "b"}, "split"}, {[]string{"é"}, "composed"}, {[]string{"e\u0301"}, "decomposed"}} {
		done := make(chan struct{}, 1)
		attach(router.Select(entry.path), bitwire.Receiver{Message: func([]string, bitwire.Message) { result = append(result, entry.name); done <- struct{}{} }})
		send(client, entry.path, done)
	}
	return result
}
func selectedEndpoints() any {
	client, root := pair()
	router := dispatcher(root)
	a := router.Select([]string{"scope"}).Select([]string{"a"})
	b := router.Select([]string{"scope", "b"})
	detached := router.Select([]string{"detached"})
	notifications := map[string]int{"active": 0, "detached": 0, "sibling": 0}
	nestedPath, selectedSendPath := []string{}, []string{}
	siblingAfterViewClose, routeReusable := false, false
	initial := attach(a, bitwire.Receiver{Message: func([]string, bitwire.Message) { panic("detached receiver ran") }})
	_, err := a.Receive(bitwire.Receiver{})
	duplicateReceiveRefused := err != nil
	initial()
	initial()
	done := make(chan struct{}, 4)
	rootClosed := make(chan struct{}, 1)
	attach(a, bitwire.Receiver{Message: func(path []string, _ bitwire.Message) { nestedPath = copyPath(path); done <- struct{}{} }, Closed: func(bitwire.Code, string) { notifications["active"]++ }})
	initial()
	attach(b, bitwire.Receiver{Message: func([]string, bitwire.Message) { siblingAfterViewClose = true; done <- struct{}{} }, Closed: func(bitwire.Code, string) { notifications["sibling"]++; rootClosed <- struct{}{} }})
	unused := attach(detached, bitwire.Receiver{Closed: func(bitwire.Code, string) { notifications["detached"]++ }})
	unused()
	closeWire(detached)
	closeWire(detached)
	attach(client, bitwire.Receiver{Message: func(path []string, _ bitwire.Message) { selectedSendPath = copyPath(path); done <- struct{}{} }})
	send(client, []string{"scope", "a", "in"}, done)
	send(a, []string{"out"}, done)
	closeWire(a)
	closeWire(a)
	_, err = a.Receive(bitwire.Receiver{})
	closedReceiveRefused := err != nil
	closedSendRefused := a.Send([]string{"ignored"}, event()) != nil
	stop := attach(router.Select([]string{"scope", "a"}), bitwire.Receiver{Message: func([]string, bitwire.Message) { routeReusable = true; done <- struct{}{} }})
	send(client, []string{"scope", "a", "replacement"}, done)
	send(client, []string{"scope", "b", "sibling"}, done)
	stop()
	closeWire(root)
	closeWire(root)
	wait(rootClosed)
	_, err = root.Receive(bitwire.Receiver{})
	rootReceiveRefused := err != nil
	_, err = b.Receive(bitwire.Receiver{})
	viewAfterRootCloseRefused := err != nil
	closeWire(b)
	return map[string]any{"nestedPath": nestedPath, "selectedSendPath": selectedSendPath, "duplicateReceiveRefused": duplicateReceiveRefused, "notifications": notifications, "closedReceiveRefused": closedReceiveRefused, "closedSendRefused": closedSendRefused, "rootReceiveRefused": rootReceiveRefused, "viewAfterRootCloseRefused": viewAfterRootCloseRefused, "siblingAfterViewClose": siblingAfterViewClose, "routeReusable": routeReusable}
}

type replySink func([]string, bitwire.Message) error

func (s replySink) Send(path []string, m bitwire.Message) error { return s(path, m) }
func sameIDDelayedReplies() any {
	client, root := pair()
	var admitted *bitwire.ReturnAddress
	router := dispatcher(observer{Endpoint: root, receive: func(_ []string, m bitwire.Message) { admitted = m.Return }})
	view := router.Select([]string{"service"})
	requests := []bitwire.Message{}
	capturedReturnIdentities := []bool{}
	done := make(chan struct{}, 2)
	stop := attach(view, bitwire.Receiver{Message: func(_ []string, m bitwire.Message) {
		requests = append(requests, m)
		capturedReturnIdentities = append(capturedReturnIdentities, m.Return != nil && m.Return == admitted)
		done <- struct{}{}
	}})
	lateReplies, replyIDs := []string{}, []string{}
	replied := make(chan struct{}, 2)
	for _, entry := range []struct{ name, payload string }{{"left", "first"}, {"right", "second"}} {
		sink := replySink(func(_ []string, m bitwire.Message) error {
			if m.Frame.Kind != bitwire.ProfileResponse {
				panic("expected reply")
			}
			var result string
			check(json.Unmarshal(m.Frame.Result, &result))
			lateReplies = append(lateReplies, entry.name+":"+result)
			replyIDs = append(replyIDs, m.Frame.ID)
			replied <- struct{}{}
			return nil
		})
		payload, err := json.Marshal(entry.payload)
		check(err)
		check(duplex.At(client, []string{"service"}).Send([]string{"call"}, bitwire.Message{Frame: bitwire.ProfileFrame{Version: 1, Kind: bitwire.ProfileRequest, ID: "c:1", Params: payload}, Return: &bitwire.ReturnAddress{Wire: sink}}))
		wait(done)
	}
	stop()
	closeWire(view)
	replacementDeliveries := []string{}
	attach(router.Select([]string{"service"}), bitwire.Receiver{Message: func(_ []string, m bitwire.Message) {
		if m.Frame.Kind != bitwire.ProfileEvent {
			panic("old request reached replacement")
		}
		var value string
		check(json.Unmarshal(m.Frame.Data, &value))
		replacementDeliveries = append(replacementDeliveries, value)
		done <- struct{}{}
	}})
	check(client.Send([]string{"service", "probe"}, bitwire.Message{Frame: bitwire.ProfileFrame{Version: 1, Kind: bitwire.ProfileEvent, Data: json.RawMessage(`"probe"`)}}))
	wait(done)
	for i := len(requests) - 1; i >= 0; i-- {
		reply(requests[i], requests[i].Frame.Params)
		wait(replied)
	}
	return map[string]any{"capturedReturnIdentities": capturedReturnIdentities, "lateReplies": lateReplies, "replyIDs": replyIDs, "replacementDeliveries": replacementDeliveries}
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
	observations := map[string]any{"siblings": siblings(), "overlap": overlap(), "composition": composition(), "opaquePaths": opaquePaths(), "selectedEndpoints": selectedEndpoints(), "sameIDDelayedReplies": sameIDDelayedReplies()}
	check(json.NewEncoder(os.Stdout).Encode(observations))
}
