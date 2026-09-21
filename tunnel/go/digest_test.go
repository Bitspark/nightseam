package tunnel_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/tunnel/go"
)

func TestChannelDigestRefusedBeforeAdmission(t *testing.T) {
	first, second := strings.Repeat("a", 64), strings.Repeat("b", 64)
	contracts := map[string]string{"probe": first}
	client, server := peers(t, tunnel.Options{Contracts: contracts, AcceptCapacity: 1})
	contracts["probe"] = second // Configuration is captured, not consumer-owned mutable state.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if channel, err := client.OpenConnection(ctx, "probe", second); err == nil || channel != nil {
		t.Fatalf("different declaration admitted: %v, %v", channel, err)
	} else {
		var public *runtime.PublicError
		if !errors.As(err, &public) || public.Code != "contract_mismatch" || !strings.Contains(public.Message, "probe") {
			t.Fatalf("wrong identity refusal: %v", err)
		}
	}
	opened, err := client.OpenConnection(ctx, "probe", first)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := server.AcceptConnection(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Digest != first || accepted.Digest != first || opened.ID != accepted.ID {
		t.Fatalf("identity changed or refused channel remained pending: %+v, %+v", opened, accepted)
	}
}

func TestChannelDigestAbsenceAndMalformedValues(t *testing.T) {
	digest := strings.Repeat("a", 64)
	client, server := peers(t, tunnel.Options{Contracts: map[string]string{"probe": digest}})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for _, row := range []struct{ family, digest string }{{"probe", ""}, {"untyped", digest}} {
		opened, err := client.OpenConnection(ctx, row.family, row.digest)
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := server.AcceptConnection(ctx)
		if err != nil || accepted.Digest != row.digest || accepted.ID != opened.ID {
			t.Fatalf("absent identity: %+v %v", accepted, err)
		}
	}
	for _, malformed := range []any{"", "short", strings.Repeat("A", 64), 7, nil} {
		var result any
		err := client.Peer().Call(ctx, tunnel.OpenMethod, map[string]any{"channel": 101, "family": "probe", "digest": malformed, "window": 1}, &result)
		var public *runtime.PublicError
		if !errors.As(err, &public) || public.Code != tunnel.ErrorInvalid {
			t.Fatalf("malformed digest %v: %v", malformed, err)
		}
	}
}
