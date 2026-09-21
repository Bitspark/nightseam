package auth

import (
	"encoding/json"
	"strings"

	"github.com/Bitspark/nightseam/auth/go/grant"
	"github.com/Bitspark/nightseam/runtime/go"
)

// Code is a refusal's code under the reserved prefix auth.: the closed set
// of docs/auth/connection.md and docs/auth/exposure.md, which a program
// branches on. A refusal at the owner's effect carries the owner's own
// code, under the prefix owner:.
type Code string

// The codes of the authenticated connection (docs/auth/connection.md).
const (
	Unsupported       Code = "auth.unsupported"        // the connection has no audience: a pipe cannot run the exchange
	Established       Code = "auth.established"        // the context is immutable; a change of subject is a new connection
	Malformed         Code = "auth.malformed"          // a nonce, key or proof of the wrong size
	NoChallenge       Code = "auth.no_challenge"       // no pending challenge: one attempt per challenge
	PossessionInvalid Code = "auth.possession_invalid" // the proof does not verify for this subject, audience and nonce
	ChainRefused      Code = "auth.chain_refused"      // the chain does not hold under grant.Inspect; Grant says why
	SubjectMismatch   Code = "auth.subject_mismatch"   // the chain's leaf is not the proved subject
	Unauthenticated   Code = "auth.unauthenticated"    // a protected call before a context exists
	Denied            Code = "auth.denied"             // the chain does not hold or does not cover; Grant says why
)

// The codes of the exposure (docs/auth/exposure.md).
const (
	ContractMismatch Code = "auth.contract_mismatch" // the policy names another family or digest
	MemberUnbound    Code = "auth.member_unbound"    // a declared member has no treatment
	MemberUndeclared Code = "auth.member_undeclared" // a treatment names a member the surface does not declare
	TemplateInvalid  Code = "auth.template_invalid"  // a treatment is malformed
	UnknownMember    Code = "auth.unknown_member"    // a call names a member the surface does not declare
	MemberDenied     Code = "auth.member_denied"     // a denied member: refused for everyone
	SelectorInvalid  Code = "auth.selector_invalid"  // the request's payload does not render a scope
	ReferenceUnknown Code = "auth.reference_unknown" // a reference this exposure did not export
)

// OwnerPrefix is what a refusal at the owner's effect carries before the
// owner's own reason.
const OwnerPrefix = "owner:"

// Refusal is what the connection and the exposure answer instead of a
// context, a verified call or a decision: a code, and for ChainRefused and
// Denied the grant's own refusal — its code and hop, nothing more. Both
// packets spell it the same way, so it is one type here.
type Refusal struct {
	Code  Code
	Grant *grant.Refusal
}

// Public is the refusal as the profile answers it on the wire: the code,
// a fixed sentence that names no member of any grant, and for a refusal
// that carries the grant's the grant code and hop as data.
func (r *Refusal) Public() *runtime.PublicError {
	public := &runtime.PublicError{Code: string(r.Code), Message: message(r.Code)}
	if r.Grant != nil {
		data, _ := json.Marshal(struct {
			Code grant.Code `json:"code"`
			Hop  int        `json:"hop"`
		}{r.Grant.Code, r.Grant.Hop})
		public.Data = data
	}
	return public
}

// message is the one sentence each code crosses the wire with.
func message(code Code) string {
	switch code {
	case Unsupported:
		return "This connection has no audience and cannot run the exchange."
	case Established:
		return "This connection already has a context."
	case Malformed:
		return "A parameter is malformed."
	case NoChallenge:
		return "No challenge is pending."
	case PossessionInvalid:
		return "The possession proof does not verify."
	case ChainRefused:
		return "The chain does not hold."
	case SubjectMismatch:
		return "The chain does not end at the connection's subject."
	case Unauthenticated:
		return "The connection has no context."
	case Denied:
		return "The chain does not permit the request."
	case ContractMismatch:
		return "The policy is for another declaration."
	case MemberUnbound:
		return "A declared member has no treatment."
	case MemberUndeclared:
		return "A treatment names no declared member."
	case TemplateInvalid:
		return "A treatment is invalid."
	case UnknownMember:
		return "Unknown member."
	case MemberDenied:
		return "The member is denied."
	case SelectorInvalid:
		return "The request does not select a scope."
	case ReferenceUnknown:
		return "Unknown reference."
	}
	if strings.HasPrefix(string(code), OwnerPrefix) {
		return "The owner refused the effect."
	}
	return "Refused."
}
