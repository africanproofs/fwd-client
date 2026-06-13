package fwdclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
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

func TestSignFspMessageSigningPolicy(t *testing.T) {
	c, cap := newServer(t, 200, `{"message_hash":"0xmh","v":27,"r":"0xr","s":"0xs","signature":"0xsig"}`)
	sp := "0x" + repeat("ab", 40)
	_, err := c.SignFspMessage(context.Background(), SignFspMessageRequest{
		Wallet: "fsp-signing-songbird", MessageType: MessageTypeSigningPolicy, RewardEpochID: 405,
		SigningPolicy: &sp,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.body["message_type"] != "SIGNING_POLICY" || cap.body["signing_policy"] != sp {
		t.Errorf("SIGNING_POLICY body wrong: %+v", cap.body)
	}
	// Forbidden fields must be absent on the wire.
	for _, k := range []string{"chain_id", "address", "payload", "protocol_id", "registration_variant", "block_number", "deltas"} {
		if _, present := cap.body[k]; present {
			t.Errorf("SIGNING_POLICY body should omit %q, got %+v", k, cap.body)
		}
	}
}

func TestSignFspMessageVoterRegistration(t *testing.T) {
	addr := "0x" + repeat("ab", 20)
	t.Run("legacy", func(t *testing.T) {
		c, cap := newServer(t, 200, `{"message_hash":"0xmh","v":27,"r":"0xr","s":"0xs","signature":"0xsig"}`)
		variant := RegistrationVariantLegacy
		_, err := c.SignFspMessage(context.Background(), SignFspMessageRequest{
			Wallet: "fsp", MessageType: MessageTypeVoterRegistration, RewardEpochID: 1,
			Address: &addr, RegistrationVariant: &variant,
		}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cap.body["address"] != addr || cap.body["registration_variant"] != "legacy" {
			t.Errorf("legacy body wrong: %+v", cap.body)
		}
		if _, present := cap.body["chain_id"]; present {
			t.Errorf("legacy must omit chain_id, got %+v", cap.body)
		}
	})
	t.Run("chain_scoped", func(t *testing.T) {
		c, cap := newServer(t, 200, `{"message_hash":"0xmh","v":27,"r":"0xr","s":"0xs","signature":"0xsig"}`)
		variant := RegistrationVariantChainScoped
		chainID := 14
		_, err := c.SignFspMessage(context.Background(), SignFspMessageRequest{
			Wallet: "fsp", MessageType: MessageTypeVoterRegistration, RewardEpochID: 1,
			Address: &addr, RegistrationVariant: &variant, ChainID: &chainID,
		}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cap.body["registration_variant"] != "chain_scoped" || cap.body["chain_id"].(float64) != 14 {
			t.Errorf("chain_scoped body wrong: %+v", cap.body)
		}
	})
}

func TestSignFspMessageProtocolPayload(t *testing.T) {
	c, cap := newServer(t, 200, `{"message_hash":"0xmh","v":27,"r":"0xr","s":"0xs","signature":"0xsig"}`)
	payload := "0xdeadbeef"
	pid := 200
	_, err := c.SignFspMessage(context.Background(), SignFspMessageRequest{
		Wallet: "fsp", MessageType: MessageTypeProtocolPayload, RewardEpochID: 1,
		Payload: &payload, ProtocolID: &pid,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.body["payload"] != payload || cap.body["protocol_id"].(float64) != 200 {
		t.Errorf("PROTOCOL_PAYLOAD body wrong: %+v", cap.body)
	}
	// protocol_id is optional — a payload-only request is valid and omits it.
	c2, cap2 := newServer(t, 200, `{"message_hash":"0xmh","v":27,"r":"0xr","s":"0xs","signature":"0xsig"}`)
	_, err = c2.SignFspMessage(context.Background(), SignFspMessageRequest{
		Wallet: "fsp", MessageType: MessageTypeProtocolPayload, RewardEpochID: 1, Payload: &payload,
	}, "")
	if err != nil {
		t.Fatalf("payload-only should be valid: %v", err)
	}
	if _, present := cap2.body["protocol_id"]; present {
		t.Errorf("payload-only must omit protocol_id, got %+v", cap2.body)
	}
}

func TestSignFspMessageFastUpdate(t *testing.T) {
	c, cap := newServer(t, 200, `{"message_hash":"0xmh","v":27,"r":"0xr","s":"0xs","signature":"0xsig"}`)
	deltas := "0x00ff"
	// A uint256 value that overflows uint64, proving *big.Int carries full width.
	big1, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	_, err := c.SignFspMessage(context.Background(), SignFspMessageRequest{
		Wallet: "fsp", MessageType: MessageTypeFastUpdate, RewardEpochID: 1,
		BlockNumber: big.NewInt(42), Replicate: big.NewInt(1),
		GammaX: big1, GammaY: big.NewInt(2), C: big.NewInt(3), S: big.NewInt(4),
		Deltas: &deltas,
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.body["message_type"] != "FAST_UPDATE" || cap.body["deltas"] != "0x00ff" {
		t.Errorf("FAST_UPDATE body wrong: %+v", cap.body)
	}
	// The uint256 gamma_x must survive as the exact JSON number (no float clip).
	if got := numFromRaw(t, cap.bodyRaw, "gamma_x"); got != "123456789012345678901234567890" {
		t.Errorf("gamma_x lost precision: got %s", got)
	}
	if numFromRaw(t, cap.bodyRaw, "block_number") != "42" {
		t.Errorf("block_number wrong: %+v", cap.body)
	}
}

func TestSignFspMessageValidationPreHTTP(t *testing.T) {
	c, cap := newServer(t, 500, `should-not-be-hit`)
	chainID := 19
	bad := "0xnothex"
	badAddr := "0x123"
	okAddr := "0x" + repeat("ab", 20)
	sp := "0x" + repeat("ab", 40)
	payload := "0xdead"
	deltas := "0x00"
	legacy := RegistrationVariantLegacy
	scoped := RegistrationVariantChainScoped
	tests := []struct {
		name string
		req  SignFspMessageRequest
	}{
		{"uptime with rd field", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeUptime, RewardEpochID: 1, ChainID: &chainID}},
		{"uptime with payload", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeUptime, RewardEpochID: 1, Payload: &payload}},
		{"rewards missing fields", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeRewards, RewardEpochID: 1}},
		{"rewards bad hash", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeRewards, RewardEpochID: 1, ChainID: &chainID, NoOfWeightBasedClaims: ptrInt(1), RewardsHash: &bad}},
		{"rewards with payload", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeRewards, RewardEpochID: 1, ChainID: &chainID, NoOfWeightBasedClaims: ptrInt(1), RewardsHash: ptrHex32(), Payload: &payload}},
		{"signing_policy missing", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeSigningPolicy, RewardEpochID: 1}},
		{"signing_policy with chain_id", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeSigningPolicy, RewardEpochID: 1, SigningPolicy: &sp, ChainID: &chainID}},
		{"voter_reg missing address", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeVoterRegistration, RewardEpochID: 1, RegistrationVariant: &legacy}},
		{"voter_reg bad address", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeVoterRegistration, RewardEpochID: 1, Address: &badAddr, RegistrationVariant: &legacy}},
		{"voter_reg missing variant", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeVoterRegistration, RewardEpochID: 1, Address: &okAddr}},
		{"voter_reg scoped missing chain_id", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeVoterRegistration, RewardEpochID: 1, Address: &okAddr, RegistrationVariant: &scoped}},
		{"voter_reg legacy with chain_id", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeVoterRegistration, RewardEpochID: 1, Address: &okAddr, RegistrationVariant: &legacy, ChainID: &chainID}},
		{"protocol_payload missing", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeProtocolPayload, RewardEpochID: 1}},
		{"protocol_payload with address", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeProtocolPayload, RewardEpochID: 1, Payload: &payload, Address: &okAddr}},
		{"fast_update missing fields", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeFastUpdate, RewardEpochID: 1, BlockNumber: big.NewInt(1)}},
		{"fast_update with reward field", SignFspMessageRequest{Wallet: "w", MessageType: MessageTypeFastUpdate, RewardEpochID: 1, BlockNumber: big.NewInt(1), Replicate: big.NewInt(1), GammaX: big.NewInt(1), GammaY: big.NewInt(1), C: big.NewInt(1), S: big.NewInt(1), Deltas: &deltas, RewardsHash: ptrHex32()}},
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

func ptrHex32() *string { s := "0x" + repeat("ab", 32); return &s }

// numFromRaw decodes raw JSON with UseNumber (no float clipping) and returns the
// named top-level numeric field as its exact string form.
func numFromRaw(t *testing.T, raw []byte, key string) string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	n, ok := m[key].(json.Number)
	if !ok {
		t.Fatalf("field %q is not a json.Number: %v", key, m[key])
	}
	return n.String()
}

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
