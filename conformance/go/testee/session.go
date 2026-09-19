package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Bitspark/nightseam/runtime/go"
	"github.com/Bitspark/nightseam/session/go"
)

// A registry under control, with every change it made held for the asking.
type registry struct {
	*session.Registry
	changes *inbox[session.Change]
	stop    func()
}

func (r *registry) shutdown() { r.stop() }

// An attachment under control.
type attachment struct {
	*session.Attachment
	handle string
}

func (a *attachment) shutdown() { a.Detach() }

func (t *testee) registryOf(r request, name string) (*registry, error) {
	handle, err := r.mustString(name)
	if err != nil {
		return nil, err
	}
	object, ok := t.lookup(handle)
	if !ok {
		return nil, fail("unknown_handle", "%s", handle)
	}
	reg, ok := object.(*registry)
	if !ok {
		return nil, invalid("%s is not a registry", handle)
	}
	return reg, nil
}

// normalizeChange renders a change as DRIVER.md says: its kind, its
// session, and of origin, role, sequence, method and trace what it carries.
func (t *testee) normalizeChange(c session.Change, withTrace bool) map[string]any {
	m := map[string]any{"kind": c.Kind.String(), "session": c.Session}
	if c.Attachment != nil {
		m["origin"] = c.Attachment.Origin
		m["role"] = roleName(c.Attachment.Role)
		if handle := t.handleOf(c.Attachment); handle != "" {
			m["attachment"] = handle
		}
	}
	if c.Sequence != 0 {
		m["sequence"] = c.Sequence
	}
	if c.Method != "" {
		m["method"] = c.Method
	}
	if withTrace && c.Trace.Parent != "" {
		parts := strings.Split(c.Trace.Parent, "-")
		if len(parts) == 4 {
			split := map[string]any{"trace_id": parts[1], "span_id": parts[2], "flags": parts[3]}
			if c.Trace.State != "" {
				split["state"] = c.Trace.State
			}
			m["trace"] = split
		}
	}
	return m
}

// handleOf is the handle an attachment was minted under, or "" for one the
// runner never held.
func (t *testee) handleOf(a *session.Attachment) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	for handle, object := range t.handles {
		if wrapped, ok := object.(*attachment); ok && wrapped.Attachment == a {
			return handle
		}
	}
	return ""
}

func (t *testee) sessionOps() map[string]func(request) (any, error) {
	return map[string]func(request) (any, error){
		"session.new": func(r request) (any, error) {
			raw, err := r.object("options")
			if err != nil {
				return nil, err
			}
			options := session.Options{}
			for key, value := range raw {
				var n int64
				if err := json.Unmarshal(value, &n); err != nil {
					return nil, invalid("options.%s is an integer", key)
				}
				switch key {
				case "max_attachments":
					options.MaxAttachments = int(n)
				case "max_inflight":
					options.MaxInflight = int(n)
				case "send_timeout_ms":
					options.SendTimeout = time.Duration(n) * time.Millisecond
				default:
					return nil, unsupported("session option " + key)
				}
			}
			reg := &registry{Registry: session.New(options), changes: newInbox[session.Change]()}
			reg.stop = reg.OnChange(func(c session.Change) { reg.changes.put(c) })
			return map[string]any{"handle": t.mint("reg", reg)}, nil
		},
		"session.bind": func(r request) (any, error) {
			reg, err := t.registryOf(r, "on")
			if err != nil {
				return nil, err
			}
			id, err := r.mustString("session")
			if err != nil {
				return nil, err
			}
			c, ch, err := t.channelOf(r, "channel")
			if err != nil {
				return nil, err
			}
			if !c.lazy {
				return nil, invalid("a session takes a lazily consumed channel, since it reads it itself")
			}
			var governance struct {
				Decides []string `json:"decides"`
				Asks    []string `json:"asks"`
			}
			if raw := r.raw("governance"); raw == nil {
				return nil, invalid("governance is required")
			} else if err := json.Unmarshal(raw, &governance); err != nil {
				return nil, invalid("governance names decides and asks")
			}
			decides, asks := set(governance.Decides), set(governance.Asks)
			logOptions, err := r.object("log")
			if err != nil {
				return nil, err
			}
			var maxFrameBytes int64 = 1 << 20
			if raw, ok := logOptions["max_frame_bytes"]; ok {
				if err := json.Unmarshal(raw, &maxFrameBytes); err != nil {
					return nil, invalid("log.max_frame_bytes is an integer")
				}
			}
			log := session.NewMemoryLog(maxFrameBytes)
			// A log with frames in it before anything is bound, which is what a
			// durable one holds after the process that wrote them ended; it is
			// filled through the Log interface, as the component's own suites
			// fill theirs.
			if raw, ok := logOptions["prefill"]; ok {
				var prefill []struct {
					Direction string `json:"direction"`
					Origin    string `json:"origin"`
					Text      string `json:"text"`
				}
				if err := json.Unmarshal(raw, &prefill); err != nil {
					return nil, invalid("log.prefill is a list of frames")
				}
				for _, frame := range prefill {
					direction := session.Down
					switch frame.Direction {
					case "", "down":
					case "up":
						direction = session.Up
					default:
						return nil, invalid("log.prefill direction is up or down")
					}
					if !json.Valid([]byte(frame.Text)) {
						return nil, invalid("log.prefill names each frame by its message, as text")
					}
					if _, err := log.Append(context.Background(), session.Frame{Direction: direction,
						Origin: frame.Origin, At: time.Now().UTC(), Message: json.RawMessage(frame.Text)}); err != nil {
						return nil, sessionError(err)
					}
				}
			}
			within, err := r.within()
			if err != nil {
				return nil, err
			}
			done := make(chan error, 1)
			go func() {
				done <- reg.Bind(id, ch, session.Governance{Decides: decides, Asks: asks}, log)
			}()
			select {
			case err := <-done:
				if err != nil {
					return nil, sessionError(err)
				}
				return nil, nil
			case <-time.After(within):
				return nil, fail("timeout", "the bind did not complete within %s", within)
			}
		},
		"session.attach": func(r request) (any, error) {
			reg, err := t.registryOf(r, "on")
			if err != nil {
				return nil, err
			}
			id, err := r.mustString("session")
			if err != nil {
				return nil, err
			}
			c, ch, err := t.channelOf(r, "channel")
			if err != nil {
				return nil, err
			}
			if !c.lazy {
				return nil, invalid("a session takes a lazily consumed channel, since it reads it itself")
			}
			roleName, err := r.mustString("role")
			if err != nil {
				return nil, err
			}
			var role session.Role
			switch roleName {
			case "participant":
				role = session.Participant
			case "observer":
				role = session.Observer
			default:
				// A role that is not one is handed on rather than refused here,
				// so that a scenario holds the layer's own refusal and not the
				// testee's reading of an argument.
				role = session.Role(-1)
			}
			origin, err := r.mustString("origin")
			if err != nil {
				return nil, err
			}
			after, err := r.int("after", 0)
			if err != nil {
				return nil, err
			}
			within, err := r.within()
			if err != nil {
				return nil, err
			}
			type attached struct {
				a   *session.Attachment
				err error
			}
			done := make(chan attached, 1)
			go func() {
				a, err := reg.Attach(id, ch, role, origin, after)
				done <- attached{a, err}
			}()
			select {
			case result := <-done:
				if result.err != nil {
					return nil, sessionError(result.err)
				}
				wrapped := &attachment{Attachment: result.a}
				wrapped.handle = t.mint("att", wrapped)
				return map[string]any{"handle": wrapped.handle}, nil
			case <-time.After(within):
				return nil, fail("timeout", "the attach did not complete within %s", within)
			}
		},
		"session.control": func(r request) (any, error) {
			reg, err := t.registryOf(r, "on")
			if err != nil {
				return nil, err
			}
			id, err := r.mustString("session")
			if err != nil {
				return nil, err
			}
			var holder *session.Attachment
			if raw := r.raw("attachment"); raw != nil && string(raw) != "null" {
				handle, err := r.mustString("attachment")
				if err != nil {
					return nil, err
				}
				object, ok := t.lookup(handle)
				a, isAttachment := object.(*attachment)
				if !ok || !isAttachment {
					return nil, fail("unknown_handle", "%s is not an attachment", handle)
				}
				holder = a.Attachment
			}
			if err := reg.Control(id, holder); err != nil {
				return nil, sessionError(err)
			}
			return nil, nil
		},
		"session.attention": func(r request) (any, error) {
			reg, err := t.registryOf(r, "on")
			if err != nil {
				return nil, err
			}
			ids := reg.Attention()
			if ids == nil {
				ids = []string{}
			}
			return ids, nil
		},
		"session.changes": func(r request) (any, error) {
			reg, err := t.registryOf(r, "on")
			if err != nil {
				return nil, err
			}
			withTrace, err := r.bool("trace")
			if err != nil {
				return nil, err
			}
			drain := true
			if raw, present := r.args["drain"]; present {
				if err := json.Unmarshal(raw, &drain); err != nil {
					return nil, invalid("drain is a boolean")
				}
			}
			changes := reg.changes.drain(drain)
			out := make([]map[string]any, 0, len(changes))
			for _, c := range changes {
				out = append(out, t.normalizeChange(c, withTrace))
			}
			return out, nil
		},
		"session.await_change": func(r request) (any, error) {
			reg, err := t.registryOf(r, "on")
			if err != nil {
				return nil, err
			}
			kind, err := r.mustString("kind")
			if err != nil {
				return nil, err
			}
			within, err := r.within()
			if err != nil {
				return nil, err
			}
			withTrace, err := r.bool("trace")
			if err != nil {
				return nil, err
			}
			c, ok, _ := reg.changes.await(within, func(c session.Change) bool { return c.Kind.String() == kind })
			if !ok {
				return nil, fail("timeout", "no %s within %s", kind, within)
			}
			return t.normalizeChange(c, withTrace), nil
		},
		// attachment.state is what the relay last told this consumer of the
		// session's own vocabulary: who holds control, and where in the log
		// it stands. A testee reports it so that a scenario can hold the
		// attachment's state and the frames on the wire to each other.
		"attachment.state": func(r request) (any, error) {
			a, err := t.attachmentOf(r)
			if err != nil {
				return nil, err
			}
			state := map[string]any{"holder": nil, "sequence": a.Sequence()}
			if origin, held := a.Holder(); held {
				state["holder"] = origin
			}
			return state, nil
		},
		"attachment.detach": func(r request) (any, error) {
			a, err := t.attachmentOf(r)
			if err != nil {
				return nil, err
			}
			a.Detach()
			return nil, nil
		},
	}
}

// attachmentOf is the attachment an op names under on.
func (t *testee) attachmentOf(r request) (*attachment, error) {
	handle, err := r.mustString("on")
	if err != nil {
		return nil, err
	}
	object, ok := t.lookup(handle)
	a, isAttachment := object.(*attachment)
	if !ok || !isAttachment {
		return nil, fail("unknown_handle", "%s is not an attachment", handle)
	}
	return a, nil
}

func set(names []string) func(string) bool {
	members := map[string]bool{}
	for _, name := range names {
		members[name] = true
	}
	return func(name string) bool { return members[name] }
}

// sessionError maps what the registry refused onto the driver's codes. The
// session's own refusals carry a code of its vocabulary and travel as
// *session.Error, a response the remote answered with as a
// runtime.PublicError; DRIVER.md's "any other" rule is the same for both,
// the code verbatim.
func sessionError(err error) *failure {
	var refusal *session.Error
	if errors.As(err, &refusal) {
		return fail(refusal.Code, "%s", refusal.Message)
	}
	var public *runtime.PublicError
	if errors.As(err, &public) {
		return fail(public.Code, "%s", public.Message)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fail("timeout", "%v", err)
	}
	return fail("failed", "%v", err)
}
