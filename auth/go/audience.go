package auth

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/Bitspark/archon/sdk/go/possession"
)

// Domain is the signing domain of the connection proof. A proof in
// archon-login/1 or nightseam-grant/1, or over a bare nonce, or raw, never
// verifies here.
const Domain = "nightseam-auth/1"

// role is the connection role, the first byte of the binding.
const role byte = 0x01

// Audience derives the audience from a connection URL by the grammar of
// docs/auth/connection.md — Archon's audience grammar applied to the
// connection URL:
//
//	audience = scheme "://" host [ ":" port ] *( "/" segment )
//
// ws folds to http and wss to https; the scheme and host lowercase; a port
// that is the folded scheme's default (80, 443) is omitted; segments keep
// their case and their percent-escapes, undecoded. Refused rather than
// normalized: any byte outside printable ASCII, a scheme outside the four,
// a query, a fragment, userinfo, an empty segment — which is what a
// trailing slash is — a . or .. segment, a malformed escape, a port with a
// leading zero or outside 1..=65535. Archon's own derivation, given the
// same URL with a login tail, yields the same audience.
func Audience(url string) (string, error) {
	for i := 0; i < len(url); i++ {
		if c := url[i]; c <= 0x20 || c >= 0x7f {
			return "", fmt.Errorf("auth: URL byte %d is 0x%02x; only printable ASCII is accepted", i, c)
		}
	}
	sep := strings.Index(url, "://")
	if sep < 0 {
		return "", errors.New("auth: URL has no scheme")
	}
	scheme, defaultPort, err := foldScheme(url[:sep])
	if err != nil {
		return "", err
	}
	rest := url[sep+3:]
	if strings.ContainsAny(rest, "?#") {
		return "", errors.New("auth: URL carries a query or fragment")
	}
	authority, path := rest, ""
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		authority, path = rest[:slash], rest[slash:]
	}
	host, port, err := parseAuthority(authority)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(scheme)
	b.WriteString("://")
	b.WriteString(host)
	if port != "" && port != defaultPort {
		b.WriteByte(':')
		b.WriteString(port)
	}
	if path != "" {
		for i, segment := range strings.Split(path[1:], "/") {
			if err := checkSegment(segment); err != nil {
				return "", fmt.Errorf("auth: path segment %d: %w", i, err)
			}
			b.WriteByte('/')
			b.WriteString(segment)
		}
	}
	return b.String(), nil
}

// foldScheme lowercases and folds the scheme, answering it with the default
// port it implies.
func foldScheme(s string) (scheme, defaultPort string, err error) {
	switch strings.ToLower(s) {
	case "http", "ws":
		return "http", "80", nil
	case "https", "wss":
		return "https", "443", nil
	}
	return "", "", fmt.Errorf("auth: URL scheme %q is not http, https, ws or wss", s)
}

// parseAuthority lowercases the host and holds the port; userinfo is
// refused.
func parseAuthority(authority string) (host, port string, err error) {
	if strings.IndexByte(authority, '@') >= 0 {
		return "", "", errors.New("auth: URL carries userinfo")
	}
	if authority == "" {
		return "", "", errors.New("auth: URL has no host")
	}
	if authority[0] == '[' {
		end := strings.IndexByte(authority, ']')
		if end < 0 {
			return "", "", errors.New("auth: unterminated IPv6 literal")
		}
		literal := strings.ToLower(authority[1:end])
		if len(literal) < 2 || !strings.Contains(literal, ":") {
			return "", "", errors.New("auth: malformed IPv6 literal")
		}
		for i := 0; i < len(literal); i++ {
			if c := literal[i]; !(isHexLower(c) || c == ':' || c == '.') {
				return "", "", fmt.Errorf("auth: IPv6 literal carries byte 0x%02x", c)
			}
		}
		host = "[" + literal + "]"
		tail := authority[end+1:]
		if tail == "" {
			return host, "", nil
		}
		if tail[0] != ':' {
			return "", "", errors.New("auth: bytes after the IPv6 literal")
		}
		port, err = checkPort(tail[1:])
		return host, port, err
	}
	name := authority
	if colon := strings.IndexByte(authority, ':'); colon >= 0 {
		name = authority[:colon]
		if port, err = checkPort(authority[colon+1:]); err != nil {
			return "", "", err
		}
	}
	host = strings.ToLower(name)
	for _, label := range strings.Split(host, ".") {
		if label == "" {
			return "", "", errors.New("auth: host has an empty label")
		}
		for i := 0; i < len(label); i++ {
			if c := label[i]; !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return "", "", fmt.Errorf("auth: host carries byte 0x%02x", c)
			}
		}
	}
	return host, port, nil
}

// checkPort accepts 1..=65535 written without a leading zero.
func checkPort(p string) (string, error) {
	if p == "" || len(p) > 5 || p[0] == '0' {
		return "", fmt.Errorf("auth: port %q is refused", p)
	}
	n := 0
	for i := 0; i < len(p); i++ {
		if p[i] < '0' || p[i] > '9' {
			return "", fmt.Errorf("auth: port %q is not a number", p)
		}
		n = n*10 + int(p[i]-'0')
	}
	if n < 1 || n > 65535 {
		return "", fmt.Errorf("auth: port %q is out of range", p)
	}
	return p, nil
}

// checkSegment is RFC 3986 pchar with escapes kept as written; an empty
// segment and . and .. are refused.
func checkSegment(segment string) error {
	if segment == "" {
		return errors.New("empty segment")
	}
	if segment == "." || segment == ".." {
		return errors.New("dot segment")
	}
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		switch {
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
		case strings.IndexByte("-._~!$&'()*+,;=:@", c) >= 0:
		case c == '%':
			if i+2 >= len(segment) || !isHex(segment[i+1]) || !isHex(segment[i+2]) {
				return errors.New("malformed percent-escape")
			}
			i += 2
		default:
			return fmt.Errorf("byte 0x%02x is not allowed in a segment", c)
		}
	}
	return nil
}

func isHexLower(c byte) bool { return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') }
func isHex(c byte) bool      { return isHexLower(c) || (c >= 'A' && c <= 'F') }

// ConnectionBinding is the bytes only this connection has, which the
// connection proof is bound to: 0x01 ‖ u16(len audience) ‖ audience. The
// audience is the service's own configuration and must be a fixed point of
// Audience — a service that knows itself by another spelling has
// misconfigured itself, and it is refused here rather than failing closed
// at every proof. (connection.md spells this function Binding; the
// exposure's Binding type has that name here.)
func ConnectionBinding(audience string) ([]byte, error) {
	if audience == "" {
		return nil, errors.New("auth: no audience")
	}
	if derived, err := Audience(audience); err != nil || derived != audience {
		return nil, fmt.Errorf("auth: audience %q is not in the audience grammar", audience)
	}
	binding := make([]byte, 0, 3+len(audience))
	binding = append(binding, role)
	binding = binary.BigEndian.AppendUint16(binding, uint16(len(audience)))
	return append(binding, audience...), nil
}

// Prove makes the connection proof: possession, in Domain, of the key
// behind seed over the nonce the server minted for this connection and the
// binding only this connection has. It errors on an audience outside the
// grammar, a nonce shorter than the possession scheme admits, or a seed of
// the wrong size.
func Prove(seed []byte, audience string, nonce []byte) ([]byte, error) {
	binding, err := ConnectionBinding(audience)
	if err != nil {
		return nil, err
	}
	return possession.Prove(seed, Domain, nonce, binding)
}

// Verify reports whether proof is the connection proof by the key pubkey
// over nonce for audience. Total: every shape failure is false, and so is
// a proof relayed from another audience or replayed from another
// challenge.
func Verify(pubkey []byte, audience string, nonce, proof []byte) bool {
	binding, err := ConnectionBinding(audience)
	if err != nil {
		return false
	}
	return possession.Verify(pubkey, Domain, nonce, binding, proof)
}
