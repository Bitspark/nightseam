package grant

import (
	"crypto/sha256"

	"github.com/Bitspark/archon/sdk/go/envelope"
)

// Seal encodes g and seals the body in the grant domain with the key behind
// issuerSeed; the issuer of the grant is that key, which the body does not
// repeat. It errors on a body outside the rules and on a seed of the wrong
// size. Sealing is deterministic: the same inputs seal the same bytes.
func Seal(issuerSeed []byte, g Grant) ([]byte, error) {
	body, err := Encode(g)
	if err != nil {
		return nil, err
	}
	return envelope.Seal(issuerSeed, Domain, body)
}

// Open opens an envelope in the grant domain and decodes its body. An
// envelope that is not one, claims another domain or does not verify is
// EnvelopeInvalid; one that opens but carries a body of another version or
// a malformed body is that body's code. On a refusal nothing else is
// returned: the issuer of a grant that is refused is not a fact to act on.
func Open(env []byte) (issuer [32]byte, g Grant, code Code) {
	opened, err := envelope.Open(env, Domain)
	if err != nil || len(opened.Pubkey) != len(issuer) {
		return [32]byte{}, Grant{}, EnvelopeInvalid
	}
	g, code = Decode(opened.Payload)
	if code != "" {
		return [32]byte{}, Grant{}, code
	}
	copy(issuer[:], opened.Pubkey)
	return issuer, g, ""
}

// Digest is the SHA-256 of a whole envelope: what a child names as its
// parent, so that it names one exact signed parent and no other.
func Digest(env []byte) [32]byte {
	return sha256.Sum256(env)
}
