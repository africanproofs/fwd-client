package fwdclient

// Wire models for the fwd v1.1.0a9+ sign-only HTTP API. Field names/json tags
// mirror the fwd wire contract exactly (snake_case). No consumer-specific types
// (claim types, reward data, etc.) live here — those belong in the consumer.

// FSP message types accepted by POST /v1/sign-fsp-message.
const (
	MessageTypeUptime  = "UPTIME"
	MessageTypeRewards = "REWARD_DISTRIBUTION"
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

// SignFspMessageRequest is the POST /v1/sign-fsp-message body. For UPTIME,
// ChainID/NoOfWeightBasedClaims/RewardsHash must all be nil; for
// REWARD_DISTRIBUTION all three are required (omitempty + pointers so they are
// absent on the wire for UPTIME, matching the Python client).
type SignFspMessageRequest struct {
	Wallet                string  `json:"wallet"`
	MessageType           string  `json:"message_type"`
	RewardEpochID         uint64  `json:"reward_epoch_id"`
	ChainID               *int    `json:"chain_id,omitempty"`
	NoOfWeightBasedClaims *int    `json:"no_of_weight_based_claims,omitempty"`
	RewardsHash           *string `json:"rewards_hash,omitempty"`
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
