"""FwdClient — synchronous HTTP transport for the fwd signing daemon.

fwd v1.1.0a9+ is sign-only: fwd signs and returns a raw tx blob; the caller
broadcasts and reports back.  This module is pure transport — it does not build
calldata, broadcast transactions, or poll receipts.

Endpoints:
  POST /v1/sign-transaction          -> signs, returns blob (no broadcast)
  POST /v1/transactions/{id}/broadcast-result  -> notify fwd of broadcast result
  POST /v1/transactions/{id}/receipt           -> notify fwd of on-chain result
  GET  /v1/transactions/{id}         -> caller-scoped tx status
  POST /v1/sign-fsp-message          -> Leg-1 FSP signing (UPTIME / REWARD_DISTRIBUTION)
  GET  /healthz                      -> daemon health

Retry taxonomy (fwd v1.1.0a9+):
  Terminal (never retry): 400, 401, 403, 404, 409, 422, and any unmapped status.
  Retryable (backoff + retry): 503, and any httpx transport error.
  Note: 502 is GONE — fwd no longer does any RPC.  Unmapped statuses fail closed.
"""

from __future__ import annotations

import re

import httpx

from fwd_client.errors import raise_for_fwd_error, transport_retryable
from fwd_client.models import (
    BroadcastResultResponse,
    Health,
    ReceiptResponse,
    SignFspMessageResponse,
    SignTransactionResponse,
    TxStatus,
)

_REWARDS_HASH_RE: re.Pattern[str] = re.compile(r"^0x[0-9a-fA-F]{64}$")


class FwdClient:
    """Synchronous httpx client for the fwd signing daemon.

    Usage::

        with FwdClient("http://fwd:8080", caller_token="tok_…") as fwd:
            resp = fwd.sign_transaction(wallet="my-wallet", chain=14, …)
            # broadcast resp.signed_raw_tx yourself, then:
            fwd.report_broadcast_result(resp.tx_id, tx_hash, "accepted")
            fwd.report_receipt(resp.tx_id, tx_hash, "mined_success", block_number)
    """

    def __init__(
        self,
        base_url: str,
        caller_token: str | None,
        timeout: float = 60.0,
    ) -> None:
        self._base = base_url.rstrip("/")
        self._token = caller_token
        self._client = httpx.Client(timeout=timeout)

    def close(self) -> None:
        """Close the underlying httpx.Client."""
        self._client.close()

    def __enter__(self) -> FwdClient:
        return self

    def __exit__(self, *_exc: object) -> None:
        self.close()

    @property
    def _auth(self) -> dict[str, str]:
        return {"Authorization": f"Bearer {self._token}"} if self._token else {}

    # ------------------------------------------------------------------ #
    # Signing                                                              #
    # ------------------------------------------------------------------ #

    def sign_transaction(
        self,
        wallet: str,
        chain: int,
        to: str,
        gas: int,
        max_fee_per_gas: int,
        max_priority_fee_per_gas: int,
        data: str = "0x",
        value_wei: str = "0",
        idempotency_key: str | None = None,
    ) -> SignTransactionResponse:
        """POST /v1/sign-transaction — fwd signs and returns the raw tx blob.

        fwd allocates the nonce.  The caller supplies gas + EIP-1559 fees
        (computed via its own RPC calls).  fwd does NOT broadcast.

        Returns :class:`SignTransactionResponse` with ``signed_raw_tx``
        (pass to ``eth_sendRawTransaction``) and ``tx_id`` (use for
        subsequent report-back calls).

        409 ``nonce_not_initialized`` is terminal — the operator must run
        ``clifwd nonce-init`` for this ``(wallet, chain)`` before the client
        can use it.
        """
        payload: dict[str, object] = {
            "wallet": wallet,
            "chain": chain,
            "to": to,
            "value_wei": value_wei,
            "data": data,
            "gas": gas,
            "max_fee_per_gas": max_fee_per_gas,
            "max_priority_fee_per_gas": max_priority_fee_per_gas,
        }
        headers = dict(self._auth)
        if idempotency_key is not None:
            headers["Idempotency-Key"] = idempotency_key
        try:
            resp = self._client.post(
                f"{self._base}/v1/sign-transaction", json=payload, headers=headers
            )
        except httpx.RequestError as exc:
            raise transport_retryable(exc) from exc
        raise_for_fwd_error(resp)
        return SignTransactionResponse.model_validate(resp.json())

    def sign_fsp_message(
        self,
        wallet: str,
        message_type: str,
        reward_epoch_id: int,
        *,
        chain_id: int | None = None,
        no_of_weight_based_claims: int | None = None,
        rewards_hash: str | None = None,
        idempotency_key: str | None = None,
    ) -> SignFspMessageResponse:
        """POST /v1/sign-fsp-message — Leg-1 of the FSP signing path.

        fwd signs the FSP message (UPTIME or REWARD_DISTRIBUTION) and returns
        ``(message_hash, v, r, s, signature)``.  The caller never sees a key.

        Cross-field validation fires before any HTTP (fail-loud, D14):

        - ``UPTIME``: ``chain_id``, ``no_of_weight_based_claims``, and
          ``rewards_hash`` must ALL be ``None``.
        - ``REWARD_DISTRIBUTION``: all three must be present; ``rewards_hash``
          must match ``^0x[0-9a-fA-F]{64}$``.
        - Any other ``message_type``: ``ValueError``.
        """
        rd_fields = (chain_id, no_of_weight_based_claims, rewards_hash)
        if message_type == "UPTIME":
            if any(f is not None for f in rd_fields):
                raise ValueError(
                    "UPTIME: chain_id / no_of_weight_based_claims / rewards_hash must all be None"
                )
        elif message_type == "REWARD_DISTRIBUTION":
            if any(f is None for f in rd_fields):
                raise ValueError(
                    "REWARD_DISTRIBUTION: chain_id, no_of_weight_based_claims, "
                    "and rewards_hash are all required"
                )
            assert rewards_hash is not None  # type narrowing
            if not _REWARDS_HASH_RE.match(rewards_hash):
                raise ValueError(
                    f"rewards_hash must match ^0x[0-9a-fA-F]{{64}}$, got {rewards_hash!r}"
                )
        else:
            raise ValueError(
                f"Unknown message_type {message_type!r}; expected UPTIME or REWARD_DISTRIBUTION"
            )

        payload: dict[str, object] = {
            "wallet": wallet,
            "message_type": message_type,
            "reward_epoch_id": reward_epoch_id,
        }
        if message_type == "REWARD_DISTRIBUTION":
            payload["chain_id"] = chain_id
            payload["no_of_weight_based_claims"] = no_of_weight_based_claims
            payload["rewards_hash"] = rewards_hash

        headers = dict(self._auth)
        if idempotency_key is not None:
            headers["Idempotency-Key"] = idempotency_key

        try:
            resp = self._client.post(
                f"{self._base}/v1/sign-fsp-message", json=payload, headers=headers
            )
        except httpx.RequestError as exc:
            raise transport_retryable(exc) from exc
        raise_for_fwd_error(resp)
        return SignFspMessageResponse.model_validate(resp.json())

    # ------------------------------------------------------------------ #
    # Report-back (caller broadcasts; fwd tracks state)                   #
    # ------------------------------------------------------------------ #

    def report_broadcast_result(
        self,
        tx_id: str,
        tx_hash: str,
        outcome: str,
        error_class: str | None = None,
    ) -> BroadcastResultResponse:
        """POST /v1/transactions/{tx_id}/broadcast-result.

        Notify fwd of the result of a broadcast attempt made by the caller.

        ``outcome`` ∈ ``{accepted, rejected_releaseable, rejected_nonce_too_low}``:

        - ``accepted``: the node accepted the tx into its mempool.
        - ``rejected_releaseable``: deterministic node rejection (insufficient
          funds, etc.) — fwd releases the nonce reservation.
        - ``rejected_nonce_too_low``: the nonce fwd issued was already consumed
          (race/restart) — fwd handles the nonce correction.

        ``error_class`` is optional; set it on ``rejected_releaseable`` with
        the RPC error class name for observability.
        """
        payload: dict[str, object] = {"tx_hash": tx_hash, "outcome": outcome}
        if error_class is not None:
            payload["error_class"] = error_class
        try:
            resp = self._client.post(
                f"{self._base}/v1/transactions/{tx_id}/broadcast-result",
                json=payload,
                headers=self._auth,
            )
        except httpx.RequestError as exc:
            raise transport_retryable(exc) from exc
        raise_for_fwd_error(resp)
        return BroadcastResultResponse.model_validate(resp.json())

    def report_receipt(
        self,
        tx_id: str,
        tx_hash: str,
        outcome: str,
        block_number: int,
    ) -> ReceiptResponse:
        """POST /v1/transactions/{tx_id}/receipt.

        Notify fwd of the on-chain result after a tx was mined.

        ``outcome`` ∈ ``{mined_success, mined_reverted}``.
        """
        payload: dict[str, object] = {
            "tx_hash": tx_hash,
            "outcome": outcome,
            "block_number": block_number,
        }
        try:
            resp = self._client.post(
                f"{self._base}/v1/transactions/{tx_id}/receipt",
                json=payload,
                headers=self._auth,
            )
        except httpx.RequestError as exc:
            raise transport_retryable(exc) from exc
        raise_for_fwd_error(resp)
        return ReceiptResponse.model_validate(resp.json())

    # ------------------------------------------------------------------ #
    # Status queries                                                       #
    # ------------------------------------------------------------------ #

    def get_transaction(self, tx_id: str) -> TxStatus:
        """GET /v1/transactions/{tx_id} — caller-scoped tx status."""
        try:
            resp = self._client.get(
                f"{self._base}/v1/transactions/{tx_id}", headers=self._auth
            )
        except httpx.RequestError as exc:
            raise transport_retryable(exc) from exc
        raise_for_fwd_error(resp)
        return TxStatus.model_validate(resp.json())

    def health(self) -> Health:
        """GET /healthz — daemon health check."""
        try:
            resp = self._client.get(f"{self._base}/healthz")
        except httpx.RequestError as exc:
            raise transport_retryable(exc) from exc
        # Use the fwd taxonomy (not httpx's raise_for_status): a degraded daemon
        # returns 503 → FwdRetryableError, matching every other method + the Go
        # Health(). Otherwise a caller would catch a raw httpx.HTTPStatusError here.
        raise_for_fwd_error(resp)
        return Health.model_validate(resp.json())
