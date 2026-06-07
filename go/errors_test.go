package fwdclient

import (
	"errors"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		wantCode  string
		retryable bool
	}{
		// Nested HTTPException envelope ({"detail":{"error","message"}}) — the real fwd wire.
		{"403 nested policy_denied", 403, `{"detail":{"error":"policy_denied","message":"nope"}}`, "policy_denied", false},
		{"404 nested", 404, `{"detail":{"error":"transaction_not_found"}}`, "transaction_not_found", false},
		{"422 nested fsp", 422, `{"detail":{"error":"fsp_message_malformed","message":"bad"}}`, "fsp_message_malformed", false},
		{"409 conflict terminal", 409, `{"detail":{"error":"idempotency_conflict","message":"x"}}`, "idempotency_conflict", false},
		{"409 illegal_transition terminal", 409, `{"detail":{"error":"illegal_transition"}}`, "illegal_transition", false},
		{"409 idempotent_replay retryable", 409, `{"detail":{"error":"idempotent_replay"}}`, "idempotent_replay", true},
		{"503 vault retryable", 503, `{"detail":{"error":"vault_unavailable","message":"sealed master"}}`, "vault_unavailable", true},
		// /healthz 503 uses a detail-STRING shape, not the error-object.
		{"503 degraded detail-string", 503, `{"status":"degraded","detail":"sealed master unavailable"}`, "unknown", true},
		// FastAPI auto-validation 422 uses a detail-LIST; falls back to unknown, still terminal.
		{"422 fastapi list detail", 422, `{"detail":[{"loc":["body","wallet"],"msg":"field required"}]}`, "unknown", false},
		// Flat envelope fallback (defensive).
		{"400 flat fallback", 400, `{"error":"bad_idempotency_key","message":"too long"}`, "bad_idempotency_key", false},
		{"401 nested unauthorized", 401, `{"detail":{"error":"unauthorized","message":"missing bearer token"}}`, "unauthorized", false},
		// Unmapped status fails closed (terminal); non-JSON body -> unknown.
		{"418 unmapped fail-closed", 418, `{"detail":{"error":"teapot"}}`, "teapot", false},
		{"500 non-json fail-closed", 500, `oops`, "unknown", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := classify(tc.status, []byte(tc.body))
			if e.Status != tc.status {
				t.Errorf("status: got %d want %d", e.Status, tc.status)
			}
			if e.Code != tc.wantCode {
				t.Errorf("code: got %q want %q", e.Code, tc.wantCode)
			}
			if e.Retryable != tc.retryable {
				t.Errorf("retryable: got %v want %v", e.Retryable, tc.retryable)
			}
			// Helpers must agree with the flag through the error interface.
			var err error = e
			if IsRetryable(err) != tc.retryable {
				t.Errorf("IsRetryable: got %v want %v", IsRetryable(err), tc.retryable)
			}
			if IsTerminal(err) == tc.retryable {
				t.Errorf("IsTerminal should be the negation of retryable")
			}
		})
	}
}

func TestTransportErrorIsRetryable(t *testing.T) {
	e := transportError(errors.New("connection refused"))
	if e.Status != 0 || e.Code != "transport_error" || !e.Retryable {
		t.Fatalf("unexpected transport error: %+v", e)
	}
	if !IsRetryable(e) {
		t.Fatal("transport error must be retryable")
	}
}

func TestErrorHelpersIgnoreOtherErrors(t *testing.T) {
	plain := errors.New("not an *Error")
	if IsRetryable(plain) || IsTerminal(plain) {
		t.Fatal("a non-*Error must be neither retryable nor terminal")
	}
}
