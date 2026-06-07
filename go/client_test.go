package fwdclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type capture struct {
	method  string
	path    string
	auth    string
	idem    string
	ctype   string
	bodyRaw []byte
	body    map[string]any
	hits    int
}

// newServer returns an httptest server that records the last request and replies
// with (status, respBody). It points a Client at it (token "tok").
func newServer(t *testing.T, status int, respBody string) (*Client, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.hits++
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.auth = r.Header.Get("Authorization")
		cap.idem = r.Header.Get("Idempotency-Key")
		cap.ctype = r.Header.Get("Content-Type")
		cap.bodyRaw, _ = io.ReadAll(r.Body)
		if len(cap.bodyRaw) > 0 {
			_ = json.Unmarshal(cap.bodyRaw, &cap.body)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "tok"), cap
}

func TestSignTransaction(t *testing.T) {
	c, cap := newServer(t, 200, `{"tx_id":"t1","hash":"0xabc","signed_raw_tx":"0xf86c","nonce":7}`)
	resp, err := c.SignTransaction(context.Background(), SignTransactionRequest{
		Wallet: "claimer-songbird", Chain: 19, To: "0x" + "11" + "0000000000000000000000000000000000",
		Gas: 100000, MaxFeePerGas: 50, MaxPriorityFeePerGas: 1,
	}, "idem-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.method != "POST" || cap.path != "/v1/sign-transaction" {
		t.Errorf("verb/path: %s %s", cap.method, cap.path)
	}
	if cap.auth != "Bearer tok" {
		t.Errorf("auth header: %q", cap.auth)
	}
	if cap.idem != "idem-123" {
		t.Errorf("idempotency header: %q", cap.idem)
	}
	if cap.ctype != "application/json" {
		t.Errorf("content-type: %q", cap.ctype)
	}
	// Empty ValueWei/Data must default to "0"/"0x" on the wire.
	if cap.body["value_wei"] != "0" || cap.body["data"] != "0x" {
		t.Errorf("defaults not applied: value_wei=%v data=%v", cap.body["value_wei"], cap.body["data"])
	}
	if cap.body["wallet"] != "claimer-songbird" || cap.body["chain"].(float64) != 19 {
		t.Errorf("body fields wrong: %+v", cap.body)
	}
	if resp.TxID != "t1" || resp.SignedRawTx != "0xf86c" || resp.Nonce != 7 {
		t.Errorf("response decode: %+v", resp)
	}
}

func TestSignTransactionNoIdempotencyHeaderWhenEmpty(t *testing.T) {
	c, cap := newServer(t, 200, `{"tx_id":"t1","hash":"0x","signed_raw_tx":"0x","nonce":0}`)
	_, _ = c.SignTransaction(context.Background(), SignTransactionRequest{
		Wallet: "w", Chain: 19, To: "0x", Gas: 21000, MaxFeePerGas: 1, MaxPriorityFeePerGas: 0,
	}, "")
	if cap.idem != "" {
		t.Errorf("expected no Idempotency-Key header, got %q", cap.idem)
	}
}

func TestSignFspMessageUptime(t *testing.T) {
	c, cap := newServer(t, 200, `{"message_hash":"0xmh","v":27,"r":"0xr","s":"0xs","signature":"0xsig"}`)
	resp, err := c.SignFspMessage(context.Background(), SignFspMessageRequest{
		Wallet: "fsp-signing-songbird", MessageType: MessageTypeUptime, RewardEpochID: 404,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.path != "/v1/sign-fsp-message" {
		t.Errorf("path: %s", cap.path)
	}
	// UPTIME must omit the REWARD_DISTRIBUTION-only fields on the wire.
	for _, k := range []string{"chain_id", "no_of_weight_based_claims", "rewards_hash"} {
		if _, present := cap.body[k]; present {
			t.Errorf("UPTIME body should omit %q, got %+v", k, cap.body)
		}
	}
	if cap.body["message_type"] != "UPTIME" || resp.V != 27 {
		t.Errorf("uptime mismatch: body=%+v resp=%+v", cap.body, resp)
	}
}

func TestSignFspMessageRewardDistribution(t *testing.T) {
	c, cap := newServer(t, 200, `{"message_hash":"0xmh","v":28,"r":"0xr","s":"0xs","signature":"0xsig"}`)
	chainID := 19
	n := 56
	rh := "0x" + repeat("ab", 32)
	_, err := c.SignFspMessage(context.Background(), SignFspMessageRequest{
		Wallet: "fsp-signing-songbird", MessageType: MessageTypeRewards, RewardEpochID: 403,
		ChainID: &chainID, NoOfWeightBasedClaims: &n, RewardsHash: &rh,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.body["chain_id"].(float64) != 19 || cap.body["rewards_hash"] != rh {
		t.Errorf("REWARD_DISTRIBUTION body wrong: %+v", cap.body)
	}
}

func TestSignFspMessageValidationPreHTTP(t *testing.T) {
	c, cap := newServer(t, 500, `should-not-be-hit`)
	chainID := 19
	bad := "0xnothex"
	tests := []struct {
		name string
		req  SignFspMessageRequest
	}{
		{"uptime with rd field", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeUptime, RewardEpochID: 1, ChainID: &chainID}},
		{"rewards missing fields", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeRewards, RewardEpochID: 1}},
		{"rewards bad hash", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeRewards, RewardEpochID: 1, ChainID: &chainID, NoOfWeightBasedClaims: ptrInt(1), RewardsHash: &bad}},
		{"unknown type", SignFspMessageRequest{Wallet: "w", MessageType: "BOGUS", RewardEpochID: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.SignFspMessage(context.Background(), tc.req, "")
			if err == nil {
				t.Fatal("expected a pre-HTTP validation error")
			}
			// A validation error is a plain error, NOT an *Error (no HTTP status).
			if IsRetryable(err) || IsTerminal(err) {
				t.Errorf("validation error should not be an *Error: %v", err)
			}
		})
	}
	if cap.hits != 0 {
		t.Fatalf("server was hit %d times; validation must fire before HTTP", cap.hits)
	}
}

func TestReportBroadcastResult(t *testing.T) {
	c, cap := newServer(t, 200, `{"tx_id":"t1","status":"submitted"}`)
	resp, err := c.ReportBroadcastResult(context.Background(), "t1", BroadcastResultRequest{
		TxHash: "0x" + repeat("ab", 32), Outcome: OutcomeAccepted,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.method != "POST" || cap.path != "/v1/transactions/t1/broadcast-result" {
		t.Errorf("verb/path: %s %s", cap.method, cap.path)
	}
	if cap.idem != "" {
		t.Errorf("report endpoints must not send Idempotency-Key, got %q", cap.idem)
	}
	if cap.body["outcome"] != "accepted" || resp.Status != "submitted" {
		t.Errorf("mismatch: body=%+v resp=%+v", cap.body, resp)
	}
}

func TestReportReceipt(t *testing.T) {
	c, cap := newServer(t, 200, `{"tx_id":"t1","status":"mined"}`)
	resp, err := c.ReportReceipt(context.Background(), "t1", ReceiptRequest{
		TxHash: "0x" + repeat("ab", 32), Outcome: OutcomeMinedSuccess, BlockNumber: 12345,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.path != "/v1/transactions/t1/receipt" || cap.body["block_number"].(float64) != 12345 {
		t.Errorf("receipt mismatch: path=%s body=%+v", cap.path, cap.body)
	}
	if resp.Status != "mined" {
		t.Errorf("status: %s", resp.Status)
	}
}

func TestGetTransaction(t *testing.T) {
	c, cap := newServer(t, 200, `{"tx_id":"t1","wallet":"w","chain":19,"nonce":3,"status":"mined","hashes":[{"sequence_num":1,"hash_hex":"0xabc","submitted_at":"2026-06-07T00:00:00Z"}]}`)
	resp, err := c.GetTransaction(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.method != "GET" || cap.path != "/v1/transactions/t1" {
		t.Errorf("verb/path: %s %s", cap.method, cap.path)
	}
	if resp.Status != "mined" || len(resp.Hashes) != 1 || resp.Hashes[0].HashHex != "0xabc" {
		t.Errorf("decode: %+v", resp)
	}
}

func TestHealthNoAuth(t *testing.T) {
	c, cap := newServer(t, 200, `{"master":"ok","sealed_master":"ok","fwd":"ok"}`)
	resp, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.method != "GET" || cap.path != "/healthz" {
		t.Errorf("verb/path: %s %s", cap.method, cap.path)
	}
	if cap.auth != "" {
		t.Errorf("/healthz must not send an Authorization header, got %q", cap.auth)
	}
	if resp.Master != "ok" || resp.SealedMaster != "ok" {
		t.Errorf("health decode: %+v", resp)
	}
}

func TestHealthDegradedRetryable(t *testing.T) {
	c, _ := newServer(t, 503, `{"status":"degraded","detail":"sealed master unavailable"}`)
	_, err := c.Health(context.Background())
	if err == nil || !IsRetryable(err) {
		t.Fatalf("503 degraded health must be a retryable error, got %v", err)
	}
}

func TestErrorPathTerminal(t *testing.T) {
	c, _ := newServer(t, 403, `{"detail":{"error":"policy_denied","message":"wallet not allowed"}}`)
	_, err := c.SignTransaction(context.Background(), SignTransactionRequest{
		Wallet: "w", Chain: 19, To: "0x", Gas: 21000, MaxFeePerGas: 1, MaxPriorityFeePerGas: 0,
	}, "")
	if err == nil || !IsTerminal(err) {
		t.Fatalf("expected terminal error, got %v", err)
	}
	var fe *Error
	if !asError(err, &fe) || fe.Code != "policy_denied" {
		t.Errorf("expected policy_denied code, got %v", err)
	}
}

func TestTransportErrorRetryable(t *testing.T) {
	// Point at a closed port (httptest server created then immediately closed).
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := New(url, "tok")
	_, err := c.Health(context.Background())
	if err == nil || !IsRetryable(err) {
		t.Fatalf("a dead daemon must yield a retryable transport error, got %v", err)
	}
}

// --- tiny helpers (avoid extra imports) ---

func ptrInt(n int) *int { return &n }

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func asError(err error, target **Error) bool {
	if e, ok := err.(*Error); ok {
		*target = e
		return true
	}
	return false
}
