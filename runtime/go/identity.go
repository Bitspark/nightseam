package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/Bitspark/nightseam/internal/scalarjson"
)

// IdentityMethod is the ordinary request used before interpreting a wire
// through a declaration. It does not participate in transport negotiation.
const IdentityMethod = "identity.check"

// DeclarationIdentity names a declaration and, when specified, its generated
// SHA-256 digest. An empty Digest is omitted from the exchange.
type DeclarationIdentity struct {
	Path   string `json:"path"`
	Digest string `json:"digest,omitempty"`
}

// CheckIdentity checks the remote declaration before the caller exposes its
// model. The supplied call observes its context; a context without a deadline
// receives a 30-second limit. Only method_not_found means absent identity.
// Refusals neither retry the exchange nor close the underlying carrier.
func CheckIdentity(ctx context.Context, call func(context.Context, string, any, any) error, expected DeclarationIdentity) error {
	if err := validateIdentity(expected); err != nil {
		return err
	}
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	var raw json.RawMessage
	if err := call(ctx, IdentityMethod, expected, &raw); err != nil {
		var public *PublicError
		if errors.As(err, &public) && public != nil && public.Code == "method_not_found" {
			return nil
		}
		return err
	}
	remote, err := readIdentity(raw)
	if err != nil {
		return err
	}
	return compareIdentity(expected, remote)
}

// IdentityHandler validates the supplied declaration once and answers identity
// requests without running model code. Register it before exposing that model.
func IdentityHandler(expected DeclarationIdentity) (Handler, error) {
	if err := validateIdentity(expected); err != nil {
		return nil, err
	}
	return func(_ context.Context, _ *Peer, raw json.RawMessage) (any, error) {
		remote, err := readIdentity(raw)
		if err != nil {
			return nil, err
		}
		if err := compareIdentity(expected, remote); err != nil {
			return nil, err
		}
		return expected, nil
	}, nil
}

func invalidIdentity(message string) error {
	return &PublicError{Code: "contract_invalid", Message: message}
}

func validateIdentity(identity DeclarationIdentity) error {
	if identity.Path == "" || !utf8.ValidString(identity.Path) {
		return invalidIdentity("an identity names a nonempty Unicode declaration path")
	}
	if !validDeclarationDigest(identity.Digest) {
		return invalidIdentity("a declaration digest is lowercase SHA-256 hex")
	}
	return nil
}

func readIdentity(raw json.RawMessage) (DeclarationIdentity, error) {
	var identity DeclarationIdentity
	if scalarjson.Raw(raw) != nil {
		return identity, invalidIdentity("an identity names a nonempty Unicode declaration path")
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil || members == nil {
		return identity, invalidIdentity("a declaration identity is an object with path and optional digest")
	}
	keys := make([]string, 0, len(members))
	for key := range members {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if key != "path" && key != "digest" {
			return identity, invalidIdentity("identity: unknown field " + key)
		}
	}
	if json.Unmarshal(members["path"], &identity.Path) != nil || identity.Path == "" {
		return identity, invalidIdentity("an identity names a nonempty Unicode declaration path")
	}
	if digest, present := members["digest"]; present {
		if json.Unmarshal(digest, &identity.Digest) != nil || identity.Digest == "" {
			return identity, invalidIdentity("a declaration digest is lowercase SHA-256 hex")
		}
	}
	return identity, validateIdentity(identity)
}

func compareIdentity(expected, remote DeclarationIdentity) error {
	if expected.Path != remote.Path || expected.Digest != "" && remote.Digest != "" && expected.Digest != remote.Digest {
		return &PublicError{Code: "contract_mismatch", Message: "the declaration identity for " + expected.Path + " differs"}
	}
	return nil
}
