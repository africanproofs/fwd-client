package fwdclient

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Error is the single error type returned by every Client method on a non-200
// response or a transport failure. It mirrors the Python FwdError hierarchy
// (FwdTerminalError / FwdRetryableError) as one struct with a Retryable flag —
// idiomatic Go. Status is 0 for transport errors (no HTTP response received).
type Error struct {
	Status    int    // HTTP status, or 0 for a transport error
	Code      string // fwd error code (detail.error), or "transport_error"/"unknown"
	Message   string // human-readable detail
	Retryable bool   // true => backoff + retry is appropriate; false => operator action
}

func (e *Error) Error() string {
	return fmt.Sprintf("fwd %d %s: %s", e.Status, e.Code, e.Message)
}

// IsRetryable reports whether err is a retryable *Error (503, transport error,
// or 409 idempotent_replay). A down/restarting fwd must degrade the caller, not
// crash it.
func IsRetryable(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Retryable
}

// IsTerminal reports whether err is a terminal *Error (auth/policy/wallet/
// bad-request/state-conflict/sealed-master — operator action needed).
func IsTerminal(err error) bool {
	var e *Error
	return errors.As(err, &e) && !e.Retryable
}

// 409 sub-taxonomy by error code (mirrors the Python lib): idempotent_replay is
// a transparent cached replay (retryable); every other 409 is a true conflict
// (terminal). In practice fwd returns the cached result as 200 on replay, so a
// 409 idempotent_replay is defensive.
var retryable409 = map[string]bool{"idempotent_replay": true}

// classify turns a non-200 (status, body) into the appropriate *Error.
//
// Envelope: fwd uses FastAPI's default HTTPException rendering, so error bodies
// are NESTED — {"detail": {"error": "<code>", "message": "<msg>"}}. We parse
// detail.error/detail.message (the Python lib reads top-level and therefore
// sees "unknown" — harmless there because classification is status-driven, but
// this Go port surfaces the real code). FastAPI auto-validation 422s use a
// detail LIST; those fall back to code="unknown" + raw body, still terminal.
//
// Taxonomy: 503 => retryable; 409 => retryable iff idempotent_replay else
// terminal; 400/401/403/404/422 and any unmapped status => terminal (fail closed).
func classify(status int, body []byte) *Error {
	code, msg := parseEnvelope(body)
	switch {
	case status == 503:
		return &Error{Status: status, Code: code, Message: msg, Retryable: true}
	case status == 409:
		return &Error{Status: status, Code: code, Message: msg, Retryable: retryable409[code]}
	default:
		// 400/401/403/404/422 explicitly, plus any unmapped status: fail closed.
		return &Error{Status: status, Code: code, Message: msg, Retryable: false}
	}
}

// parseEnvelope extracts (code, message) from a fwd error body, handling the
// nested detail-object shape, a detail-string shape, and a flat shape.
func parseEnvelope(body []byte) (code, msg string) {
	var env struct {
		Detail  json.RawMessage `json:"detail"`
		Error   string          `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(body, &env) == nil {
		code, msg = env.Error, env.Message
		if len(env.Detail) > 0 {
			var d struct {
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			if json.Unmarshal(env.Detail, &d) == nil && d.Error != "" {
				code, msg = d.Error, d.Message
			} else {
				// detail may be a plain string (e.g. the 503 degraded body).
				var ds string
				if json.Unmarshal(env.Detail, &ds) == nil && ds != "" && msg == "" {
					msg = ds
				}
			}
		}
	}
	if code == "" {
		code = "unknown"
	}
	if msg == "" {
		msg = string(body)
	}
	return code, msg
}

// transportError wraps a network/transport failure (no HTTP response) as a
// retryable *Error with Status 0.
func transportError(err error) *Error {
	return &Error{
		Status:    0,
		Code:      "transport_error",
		Message:   fmt.Sprintf("%T: %v", err, err),
		Retryable: true,
	}
}
