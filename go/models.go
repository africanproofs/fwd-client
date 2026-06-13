package fwdclient

import "math/big"

// Wire models for the fwd v1.1.0a9+ sign-only HTTP API. Field names/json tags
// mirror the fwd wire contract exactly (snake_case). No consumer-specific types
// (claim types, reward data, etc.) live here — those belong in the consumer.

// FSP message types accepted by POST /v1/sign-fsp-message.
const (
	MessageTypeUptime            = "UPTIME"
	MessageTypeRewards           = "REWARD_DISTRIBUTION"
	MessageTypeSigningPolicy     = "SIGNING_POLICY"
	MessageTypeVoterRegistration = "VOTER_REGISTRATION"
	MessageTypeProtocolPayload   = "PROTOCOL_PAYLOAD"
	MessageTypeFastUpdate        = "FAST_UPDATE"
)

// Voter-registration variants (VOTER_REGISTRATION.RegistrationVariant).
const (
	RegistrationVariantLegacy      = "legacy"
	RegistrationVariantChainScoped = "chain_scoped"
)

// SignTransactionRequest is the POST /v1/sign-transaction body. fwd allocates
// the nonce; the caller supplies gas + EIP-1559 fees. ValueWei is a decimal
// string ("0" default), Data is 0x-prefixed hex ("0x" default).
type SignTransactionRequest struct {
	Wallet               string `json:"wallet"`
	Chain                int    `json:"chain"`
	To                   string `json:"to"`
	ValueWei             string `json:"value_wei"`
	Data                 string `json:"data"`
	Gas                  uint64 `json:"gas"`
	MaxFeePerGas         uint64 `json:"max_fee_per_gas"`
	MaxPriorityFeePerGas uint64 `json:"max_priority_fee_per_gas"`
}

// SignTransactionResponse is the 200 body. SignedRawTx is the 0x-prefixed
// RLP-encoded signed transaction (broadcast it yourself). TxID is used for the
// subsequent report-back calls.
type SignTransactionResponse struct {
	TxID        string `json:"tx_id"`
	Hash        string `json:"hash"`
	SignedRawTx string `json:"signed_raw_tx"`
	Nonce       uint64 `json:"nonce"`
}

// SignFspMessageRequest is the POST /v1/sign-fsp-message body. fwd reconstructs
// the messageHash from these typed fields and signs (EIP-191). Per message_type
// the per-field shape mirrors fwd's @model_validator (see SignFspMessage /
// validateFsp): every optional field is a pointer with omitempty so it is absent
// on the wire when unset, matching the Python client and fwd's
// "<TYPE> does not accept <field>" rules.
//
//   - UPTIME              — only RewardEpochID.
//   - REWARD_DISTRIBUTION — ChainID, NoOfWeightBasedClaims, RewardsHash.
//   - SIGNING_POLICY      — SigningPolicy.
//   - VOTER_REGISTRATION  — Address + RegistrationVariant ("legacy" |
//     "chain_scoped"); chain_scoped also requires ChainID, legacy forbids it.
//   - PROTOCOL_PAYLOAD    — Payload (ProtocolID optional).
//   - FAST_UPDATE         — BlockNumber, Replicate, GammaX, GammaY, C, S, Deltas.
//
// BlockNumber / Replicate / GammaX / GammaY / C / S are uint256 on the wire, so
// they are *big.Int (arbitrary precision) — encoding/json renders a big.Int as a
// JSON number. Address, SigningPolicy, Payload and Deltas are 0x-prefixed hex.
type SignFspMessageRequest struct {
	Wallet                string   `json:"wallet"`
	MessageType           string   `json:"message_type"`
	RewardEpochID         uint64   `json:"reward_epoch_id"`
	ChainID               *int     `json:"chain_id,omitempty"`
	NoOfWeightBasedClaims *int     `json:"no_of_weight_based_claims,omitempty"`
	RewardsHash           *string  `json:"rewards_hash,omitempty"`
	Address               *string  `json:"address,omitempty"`
	SigningPolicy         *string  `json:"signing_policy,omitempty"`
	Payload               *string  `json:"payload,omitempty"`
	ProtocolID            *int     `json:"protocol_id,omitempty"`
	RegistrationVariant   *string  `json:"registration_variant,omitempty"`
	BlockNumber           *big.Int `json:"block_number,omitempty"`
	Replicate             *big.Int `json:"replicate,omitempty"`
	GammaX                *big.Int `json:"gamma_x,omitempty"`
	GammaY                *big.Int `json:"gamma_y,omitempty"`
	C                     *big.Int `json:"c,omitempty"`
	S                     *big.Int `json:"s,omitempty"`
	Deltas                *string  `json:"deltas,omitempty"`
}

// SignFspMessageResponse is the 200 body (Leg-1 FSP signing). V is the recovery
// byte; R/S/Signature/MessageHash are 0x-prefixed hex.
type SignFspMessageResponse struct {
	MessageHash string `json:"message_hash"`
	V           int    `json:"v"`
	R           string `json:"r"`
	S           string `json:"s"`
	Signature   string `json:"signature"`
}

// Broadcast-result outcomes.
const (
	OutcomeAccepted            = "accepted"
	OutcomeRejectedReleaseable = "rejected_releaseable"
	OutcomeRejectedNonceTooLow = "rejected_nonce_too_low"
)

// BroadcastResultRequest is the POST /v1/transactions/{tx_id}/broadcast-result
// body. ErrorClass is optional (set it on rejected_releaseable for observability).
type BroadcastResultRequest struct {
	TxHash     string  `json:"tx_hash"`
	Outcome    string  `json:"outcome"`
	ErrorClass *string `json:"error_class,omitempty"`
}

// BroadcastResultResponse is the 200 body.
type BroadcastResultResponse struct {
	TxID   string `json:"tx_id"`
	Status string `json:"status"`
}

// Receipt outcomes.
const (
	OutcomeMinedSuccess  = "mined_success"
	OutcomeMinedReverted = "mined_reverted"
)

// ReceiptRequest is the POST /v1/transactions/{tx_id}/receipt body.
type ReceiptRequest struct {
	TxHash      string `json:"tx_hash"`
	Outcome     string `json:"outcome"`
	BlockNumber uint64 `json:"block_number"`
}

// ReceiptResponse is the 200 body.
type ReceiptResponse struct {
	TxID   string `json:"tx_id"`
	Status string `json:"status"`
}

// TxHash is one entry in a transaction's hash history.
type TxHash struct {
	SequenceNum int    `json:"sequence_num"`
	HashHex     string `json:"hash_hex"`
	SubmittedAt string `json:"submitted_at"`
}

// TxStatus is the GET /v1/transactions/{tx_id} response (caller-scoped). Unknown
// future fields are ignored by encoding/json.
type TxStatus struct {
	TxID            string   `json:"tx_id"`
	Wallet          string   `json:"wallet"`
	Chain           int      `json:"chain"`
	Nonce           uint64   `json:"nonce"`
	ContractAddress string   `json:"contract_address"`
	MethodName      string   `json:"method_name"`
	ValueWei        string   `json:"value_wei"`
	Status          string   `json:"status"`
	SubmittedAt     *string  `json:"submitted_at"`
	ConfirmedAt     *string  `json:"confirmed_at"`
	Hashes          []TxHash `json:"hashes"`
}

// Health is the GET /healthz response (informational; schema may grow).
type Health struct {
	Master       string `json:"master"`
	SealedMaster string `json:"sealed_master"`
	Fwd          string `json:"fwd"`
}
