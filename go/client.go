package fwdclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const defaultTimeout = 60 * time.Second

var (
	rewardsHashRe = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)
	addrRe        = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
)

// Client is a synchronous, keyless HTTP transport for the fwd signing daemon.
// It is safe for concurrent use by multiple goroutines (it wraps an *http.Client).
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithTimeout sets the per-request HTTP timeout (default 60s). Ignored if a
// custom *http.Client is supplied via WithHTTPClient.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if c.hc != nil {
			c.hc.Timeout = d
		}
	}
}

// WithHTTPClient supplies a custom *http.Client (e.g. with a tuned transport).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.hc = hc
		}
	}
}

// New returns a Client for the fwd daemon at baseURL, authenticating /v1/*
// requests with callerToken (pass "" for none, e.g. health-only use). The
// trailing slash on baseURL is stripped.
func New(baseURL, callerToken string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   callerToken,
		hc:      &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// do performs one request: marshal body (if any) -> set headers -> send ->
// classify non-200 -> decode 200 into out. auth toggles the bearer header;
// idemKey (if non-empty) sets the Idempotency-Key header.
func (c *Client) do(ctx context.Context, method, path string, body any, auth bool, idemKey string, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return &Error{Code: "encode_error", Message: err.Error()}
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return transportError(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth && c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return transportError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return classify(resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return &Error{Status: 200, Code: "decode_error", Message: fmt.Sprintf("%s: %v", path, err)}
		}
	}
	return nil
}

// SignTransaction signs an EVM transaction (fwd allocates the nonce and returns
// the raw blob; it does NOT broadcast). Pass idempotencyKey="" for none.
// Empty ValueWei/Data default to "0"/"0x" to match the fwd contract.
func (c *Client) SignTransaction(ctx context.Context, req SignTransactionRequest, idempotencyKey string) (*SignTransactionResponse, error) {
	if req.ValueWei == "" {
		req.ValueWei = "0"
	}
	if req.Data == "" {
		req.Data = "0x"
	}
	var out SignTransactionResponse
	if err := c.do(ctx, http.MethodPost, "/v1/sign-transaction", req, true, idempotencyKey, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SignFspMessage signs an FSP protocol message (UPTIME, REWARD_DISTRIBUTION,
// SIGNING_POLICY, VOTER_REGISTRATION, PROTOCOL_PAYLOAD, or FAST_UPDATE).
// Cross-field validation mirrors fwd's @model_validator and fires BEFORE any
// HTTP call (returns a plain error, not an *Error), rejecting malformed shapes:
// each type's required fields must be set and the fields that type forbids must
// be nil. RewardsHash must match ^0x[0-9a-fA-F]{64}$ and Address must match
// ^0x[0-9a-fA-F]{40}$.
func (c *Client) SignFspMessage(ctx context.Context, req SignFspMessageRequest, idempotencyKey string) (*SignFspMessageResponse, error) {
	if err := validateFsp(req); err != nil {
		return nil, err
	}
	var out SignFspMessageResponse
	if err := c.do(ctx, http.MethodPost, "/v1/sign-fsp-message", req, true, idempotencyKey, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func validateFsp(req SignFspMessageRequest) error {
	// FAST_UPDATE fields, grouped for the "must all be set" / "must all be nil"
	// checks (mirrors fwd's _fast_update_fields tuple).
	hasFastUpdate := req.BlockNumber != nil || req.Replicate != nil ||
		req.GammaX != nil || req.GammaY != nil || req.C != nil ||
		req.S != nil || req.Deltas != nil
	switch req.MessageType {
	case MessageTypeUptime:
		if req.ChainID != nil || req.NoOfWeightBasedClaims != nil || req.RewardsHash != nil ||
			req.Address != nil || req.SigningPolicy != nil || req.Payload != nil ||
			req.ProtocolID != nil || req.RegistrationVariant != nil || hasFastUpdate {
			return fmt.Errorf("fwdclient: UPTIME takes only reward_epoch_id")
		}
	case MessageTypeRewards:
		if req.ChainID == nil || req.NoOfWeightBasedClaims == nil || req.RewardsHash == nil {
			return fmt.Errorf("fwdclient: REWARD_DISTRIBUTION requires chain_id, no_of_weight_based_claims, rewards_hash")
		}
		if !rewardsHashRe.MatchString(*req.RewardsHash) {
			return fmt.Errorf("fwdclient: rewards_hash must match ^0x[0-9a-fA-F]{64}$, got %q", *req.RewardsHash)
		}
		if req.Address != nil || req.SigningPolicy != nil || req.Payload != nil ||
			req.ProtocolID != nil || req.RegistrationVariant != nil || hasFastUpdate {
			return fmt.Errorf("fwdclient: REWARD_DISTRIBUTION does not accept address/signing_policy/payload/protocol_id/registration_variant/fast_update fields")
		}
	case MessageTypeSigningPolicy:
		if req.SigningPolicy == nil {
			return fmt.Errorf("fwdclient: SIGNING_POLICY requires signing_policy")
		}
		if req.ChainID != nil || req.Address != nil || req.NoOfWeightBasedClaims != nil ||
			req.RewardsHash != nil || req.Payload != nil || req.ProtocolID != nil ||
			req.RegistrationVariant != nil || hasFastUpdate {
			return fmt.Errorf("fwdclient: SIGNING_POLICY does not accept chain_id/address/reward/payload/protocol_id/registration_variant/fast_update fields")
		}
	case MessageTypeVoterRegistration:
		if req.Address == nil {
			return fmt.Errorf("fwdclient: VOTER_REGISTRATION requires address")
		}
		if !addrRe.MatchString(*req.Address) {
			return fmt.Errorf("fwdclient: address must match ^0x[0-9a-fA-F]{40}$, got %q", *req.Address)
		}
		if req.RegistrationVariant == nil ||
			(*req.RegistrationVariant != RegistrationVariantLegacy && *req.RegistrationVariant != RegistrationVariantChainScoped) {
			return fmt.Errorf("fwdclient: VOTER_REGISTRATION requires registration_variant: legacy or chain_scoped")
		}
		if *req.RegistrationVariant == RegistrationVariantChainScoped && req.ChainID == nil {
			return fmt.Errorf("fwdclient: VOTER_REGISTRATION chain_scoped requires chain_id")
		}
		if *req.RegistrationVariant == RegistrationVariantLegacy && req.ChainID != nil {
			return fmt.Errorf("fwdclient: VOTER_REGISTRATION legacy must not include chain_id")
		}
		if req.NoOfWeightBasedClaims != nil || req.RewardsHash != nil || req.SigningPolicy != nil ||
			req.Payload != nil || req.ProtocolID != nil || hasFastUpdate {
			return fmt.Errorf("fwdclient: VOTER_REGISTRATION does not accept reward fields, signing_policy, payload, protocol_id, or fast_update fields")
		}
	case MessageTypeProtocolPayload:
		if req.Payload == nil {
			return fmt.Errorf("fwdclient: PROTOCOL_PAYLOAD requires payload")
		}
		if req.Address != nil || req.NoOfWeightBasedClaims != nil || req.RewardsHash != nil ||
			req.SigningPolicy != nil || req.RegistrationVariant != nil || hasFastUpdate {
			return fmt.Errorf("fwdclient: PROTOCOL_PAYLOAD does not accept address/reward/signing_policy/registration_variant/fast_update fields")
		}
	case MessageTypeFastUpdate:
		if req.BlockNumber == nil || req.Replicate == nil || req.GammaX == nil ||
			req.GammaY == nil || req.C == nil || req.S == nil || req.Deltas == nil {
			return fmt.Errorf("fwdclient: FAST_UPDATE requires block_number, replicate, gamma_x, gamma_y, c, s, deltas")
		}
		if req.NoOfWeightBasedClaims != nil || req.RewardsHash != nil || req.Address != nil ||
			req.SigningPolicy != nil || req.Payload != nil || req.ProtocolID != nil ||
			req.RegistrationVariant != nil {
			return fmt.Errorf("fwdclient: FAST_UPDATE does not accept reward/address/signing_policy/payload/protocol_id/registration_variant fields")
		}
	default:
		return fmt.Errorf("fwdclient: unknown message_type %q; expected UPTIME, REWARD_DISTRIBUTION, SIGNING_POLICY, VOTER_REGISTRATION, PROTOCOL_PAYLOAD, or FAST_UPDATE", req.MessageType)
	}
	return nil
}

// ReportBroadcastResult notifies fwd of the caller's broadcast attempt result.
func (c *Client) ReportBroadcastResult(ctx context.Context, txID string, req BroadcastResultRequest) (*BroadcastResultResponse, error) {
	var out BroadcastResultResponse
	path := "/v1/transactions/" + url.PathEscape(txID) + "/broadcast-result"
	if err := c.do(ctx, http.MethodPost, path, req, true, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReportReceipt notifies fwd of the on-chain result after a tx was mined.
func (c *Client) ReportReceipt(ctx context.Context, txID string, req ReceiptRequest) (*ReceiptResponse, error) {
	var out ReceiptResponse
	path := "/v1/transactions/" + url.PathEscape(txID) + "/receipt"
	if err := c.do(ctx, http.MethodPost, path, req, true, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTransaction reads the caller-scoped status of a transaction.
func (c *Client) GetTransaction(ctx context.Context, txID string) (*TxStatus, error) {
	var out TxStatus
	path := "/v1/transactions/" + url.PathEscape(txID)
	if err := c.do(ctx, http.MethodGet, path, nil, true, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Health probes the daemon's /healthz (no auth). A non-200 (e.g. 503 degraded)
// returns a retryable *Error.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	var out Health
	if err := c.do(ctx, http.MethodGet, "/healthz", nil, false, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}
