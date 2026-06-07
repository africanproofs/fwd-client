"""Pydantic models for the fwd v1.1.0a9+ sign-only HTTP API.

Endpoint coverage:
  POST /v1/sign-transaction          -> SignTransactionResponse
  POST /v1/transactions/{id}/broadcast-result  -> BroadcastResultResponse
  POST /v1/transactions/{id}/receipt           -> ReceiptResponse
  GET  /v1/transactions/{id}         -> TxStatus
  POST /v1/sign-fsp-message          -> SignFspMessageResponse
  GET  /healthz                      -> Health

Field names mirror the fwd wire contract exactly.  No clif-specific types
(ClaimType, RewardsData, etc.) live here — those belong in the consumer.
"""

from __future__ import annotations

from pydantic import BaseModel, ConfigDict, Field

# ---- /v1/sign-transaction ----


class SignTransactionRequest(BaseModel):
    """POST /v1/sign-transaction request body.

    fwd allocates the nonce; the caller supplies gas + EIP-1559 fees.
    fwd signs and returns the raw tx blob — it does NOT broadcast.
    """

    wallet: str
    chain: int
    to: str
    value_wei: str = "0"
    data: str = "0x"
    gas: int
    max_fee_per_gas: int
    max_priority_fee_per_gas: int


class SignTransactionResponse(BaseModel):
    """200 from POST /v1/sign-transaction.

    ``signed_raw_tx`` is the 0x-prefixed RLP-encoded signed transaction.
    ``hash`` is the locally-computed tx hash (use it to report back to fwd).
    fwd did NOT broadcast — the caller must do that.
    """

    model_config = ConfigDict(extra="ignore")

    tx_id: str
    hash: str
    signed_raw_tx: str
    nonce: int


# ---- /v1/transactions/{tx_id}/broadcast-result ----


class BroadcastResultRequest(BaseModel):
    """POST /v1/transactions/{tx_id}/broadcast-result request body.

    outcome ∈ {accepted, rejected_releaseable, rejected_nonce_too_low}.
    error_class is optional; set it on rejected_releaseable for observability.
    """

    tx_hash: str
    outcome: str
    error_class: str | None = None


class BroadcastResultResponse(BaseModel):
    """200 from POST /v1/transactions/{tx_id}/broadcast-result."""

    model_config = ConfigDict(extra="ignore")

    tx_id: str
    status: str


# ---- /v1/transactions/{tx_id}/receipt ----


class ReceiptRequest(BaseModel):
    """POST /v1/transactions/{tx_id}/receipt request body.

    outcome ∈ {mined_success, mined_reverted}.
    """

    tx_hash: str
    outcome: str
    block_number: int


class ReceiptResponse(BaseModel):
    """200 from POST /v1/transactions/{tx_id}/receipt."""

    model_config = ConfigDict(extra="ignore")

    tx_id: str
    status: str


# ---- /v1/transactions/{tx_id} ----


class TxStatus(BaseModel):
    """GET /v1/transactions/{tx_id} response (caller-scoped status query)."""

    model_config = ConfigDict(extra="allow")

    status: str
    hashes: list[dict[str, object]] = Field(default_factory=list)
    confirmed_at: str | None = None


# ---- /v1/sign-fsp-message ----


class SignFspMessageResponse(BaseModel):
    """200 from POST /v1/sign-fsp-message (Leg-1 FSP signing path)."""

    model_config = ConfigDict(extra="ignore")

    message_hash: str
    v: int
    r: str
    s: str
    signature: str


# ---- /healthz ----


class Health(BaseModel):
    """GET /healthz response (fields are informational; schema may grow)."""

    model_config = ConfigDict(extra="allow")

    master: str | None = None
    rpc: object | None = None
    fwd: str | None = None
