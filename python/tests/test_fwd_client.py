"""fwd transport: status -> terminal/retryable classification + model parsing.

Adapted from clif tests/test_fwd_client.py for the generic fwd-client package.

fwd v1.1.0a9+ sign-only API:
  - sign_transaction (fwd signs; caller broadcasts)
  - report_broadcast_result, report_receipt
  - Status taxonomy: 502 gone (fwd no longer does RPC), 409 added (terminal).
"""

from __future__ import annotations

import json

import httpx
import pytest
from fwd_client import (
    FwdClient,
    FwdRetryableError,
    FwdTerminalError,
    make_idempotency_key,
)
from fwd_client.models import BroadcastResultResponse, ReceiptResponse, SignTransactionResponse


def _client(handler: object) -> FwdClient:
    fwd = FwdClient("http://fwd:8080", "fwd_test_token")
    fwd._client = httpx.Client(transport=httpx.MockTransport(handler))  # type: ignore[arg-type]
    return fwd


def _raising_client(exc: Exception) -> FwdClient:
    def h(req: httpx.Request) -> httpx.Response:
        raise exc

    fwd = FwdClient("http://fwd:8080", "fwd_test_token")
    fwd._client = httpx.Client(transport=httpx.MockTransport(h))
    return fwd


# ---- sign_transaction: happy path ----


def test_sign_transaction_success_200() -> None:
    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "tx_id": "019e-abc",
                "hash": "0xdead",
                "signed_raw_tx": "0xf86c",
                "nonce": 7,
            },
        )

    with _client(h) as fwd:
        r = fwd.sign_transaction(
            "w",
            114,
            "0x" + "00" * 20,
            gas=500_000,
            max_fee_per_gas=100_000_000_000,
            max_priority_fee_per_gas=1_000_000_000,
            data="0x",
        )
    assert r.tx_id == "019e-abc"
    assert r.nonce == 7
    assert r.signed_raw_tx == "0xf86c"
    assert isinstance(r, SignTransactionResponse)


def test_sign_transaction_request_body() -> None:
    """Request must include gas, max_fee_per_gas, max_priority_fee_per_gas."""
    captured: list[dict[str, object]] = []

    def h(req: httpx.Request) -> httpx.Response:
        captured.append(json.loads(req.content))
        return httpx.Response(
            200,
            json={"tx_id": "t1", "hash": "0x1", "signed_raw_tx": "0xabc", "nonce": 0},
        )

    with _client(h) as fwd:
        fwd.sign_transaction(
            wallet="claim-wallet",
            chain=114,
            to="0x" + "cc" * 20,
            gas=200_000,
            max_fee_per_gas=50_000_000_000,
            max_priority_fee_per_gas=1_000_000_000,
            data="0xcafe",
            value_wei="0",
            idempotency_key="idem-123",
        )

    body = captured[0]
    assert body["wallet"] == "claim-wallet"
    assert body["chain"] == 114
    assert body["gas"] == 200_000
    assert body["max_fee_per_gas"] == 50_000_000_000
    assert body["max_priority_fee_per_gas"] == 1_000_000_000
    assert body["data"] == "0xcafe"
    assert body["value_wei"] == "0"


def test_sign_transaction_idempotency_key_header() -> None:
    def h(req: httpx.Request) -> httpx.Response:
        assert req.headers.get("Idempotency-Key") == "idem-abc"
        return httpx.Response(
            200,
            json={"tx_id": "t", "hash": "0x1", "signed_raw_tx": "0x2", "nonce": 0},
        )

    with _client(h) as fwd:
        fwd.sign_transaction(
            "w",
            114,
            "0x" + "00" * 20,
            gas=100_000,
            max_fee_per_gas=10_000_000_000,
            max_priority_fee_per_gas=1_000_000_000,
            idempotency_key="idem-abc",
        )


# ---- sign_transaction: error status mapping ----


@pytest.mark.parametrize(
    "code,err",
    [
        (400, "tx_params_rejected"),
        (401, "unauthorized"),
        (403, "policy_denied"),
        (404, "wallet_not_found"),
        (409, "nonce_not_initialized"),  # operator must run nonce-init
        (422, "transaction_rejected"),
    ],
)
def test_sign_transaction_terminal_statuses(code: int, err: str) -> None:
    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(code, json={"error": err, "message": "nope"})

    with _client(h) as fwd:
        with pytest.raises(FwdTerminalError) as ei:
            fwd.sign_transaction(
                "w",
                114,
                "0x" + "00" * 20,
                gas=100_000,
                max_fee_per_gas=10_000_000_000,
                max_priority_fee_per_gas=1_000_000_000,
            )
    assert ei.value.status == code
    assert ei.value.error_code == err


def test_sign_transaction_503_retryable() -> None:
    """503 (vault_unavailable) is the only retryable fwd status in v1.1.0a9+."""

    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(503, json={"error": "vault_unavailable", "message": "down"})

    with _client(h) as fwd:
        with pytest.raises(FwdRetryableError) as ei:
            fwd.sign_transaction(
                "w",
                114,
                "0x" + "00" * 20,
                gas=100_000,
                max_fee_per_gas=10_000_000_000,
                max_priority_fee_per_gas=1_000_000_000,
            )
    assert ei.value.status == 503


def test_sign_transaction_502_is_now_terminal() -> None:
    """502 is GONE in fwd v1.1.0a9+ (fwd no longer does RPC). Unmapped statuses
    fall through to the terminal catch-all in raise_for_fwd_error."""

    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(502, json={"error": "old_rpc_unreachable", "message": "gone"})

    with _client(h) as fwd:
        with pytest.raises(FwdTerminalError):
            fwd.sign_transaction(
                "w",
                114,
                "0x" + "00" * 20,
                gas=100_000,
                max_fee_per_gas=10_000_000_000,
                max_priority_fee_per_gas=1_000_000_000,
            )


def test_unmapped_status_fails_closed_terminal() -> None:
    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(418, text="teapot")

    with _client(h) as fwd:
        with pytest.raises(FwdTerminalError):
            fwd.sign_transaction(
                "w",
                114,
                "0x" + "00" * 20,
                gas=100_000,
                max_fee_per_gas=10_000_000_000,
                max_priority_fee_per_gas=1_000_000_000,
            )


# ---- sign_transaction: transport errors ----


@pytest.mark.parametrize(
    "exc",
    [
        httpx.ConnectError("down"),
        httpx.ReadTimeout("slow"),
        httpx.PoolTimeout("pool"),
    ],
)
def test_transport_error_is_retryable_sign_path(exc: Exception) -> None:
    """A down/restarting fwd must not propagate a raw httpx error.
    It must be converted to FwdRetryableError so callers can backoff gracefully."""
    with _raising_client(exc) as fwd:
        with pytest.raises(FwdRetryableError) as ei:
            fwd.sign_transaction(
                "w",
                114,
                "0x" + "00" * 20,
                gas=100_000,
                max_fee_per_gas=10_000_000_000,
                max_priority_fee_per_gas=1_000_000_000,
            )
    assert ei.value.error_code == "transport_error"


def test_transport_error_is_retryable_status_path() -> None:
    with _raising_client(httpx.ConnectError("down")) as fwd:
        with pytest.raises(FwdRetryableError):
            fwd.get_transaction("019e-abc")


# ---- sign_transaction: 409 surfaces clearly ----


def test_nonce_not_initialized_surfaces_clearly() -> None:
    """409 must surface as FwdTerminalError with nonce_not_initialized error_code.
    Callers must distinguish this from policy_denied to give the operator the
    right remediation action (run `clifwd nonce-init`, not check policy)."""

    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(
            409,
            json={
                "error": "nonce_not_initialized",
                "message": "run clifwd nonce-init for this wallet+chain",
            },
        )

    with _client(h) as fwd:
        with pytest.raises(FwdTerminalError) as ei:
            fwd.sign_transaction(
                "w",
                14,
                "0x" + "00" * 20,
                gas=100_000,
                max_fee_per_gas=10_000_000_000,
                max_priority_fee_per_gas=1_000_000_000,
            )
    assert ei.value.status == 409
    assert ei.value.error_code == "nonce_not_initialized"


# ---- report_broadcast_result ----


def test_report_broadcast_result_accepted() -> None:
    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"tx_id": "tx-1", "status": "broadcast_accepted"})

    with _client(h) as fwd:
        r = fwd.report_broadcast_result("tx-1", "0xhash", "accepted")
    assert r.tx_id == "tx-1"
    assert isinstance(r, BroadcastResultResponse)


def test_report_broadcast_result_rejected_releaseable_body() -> None:
    captured: list[dict[str, object]] = []

    def h(req: httpx.Request) -> httpx.Response:
        captured.append(json.loads(req.content))
        return httpx.Response(200, json={"tx_id": "tx-2", "status": "nonce_released"})

    with _client(h) as fwd:
        fwd.report_broadcast_result("tx-2", "0xhash", "rejected_releaseable", "RpcError")

    body = captured[0]
    assert body["outcome"] == "rejected_releaseable"
    assert body["error_class"] == "RpcError"
    assert body["tx_hash"] == "0xhash"


def test_report_broadcast_result_no_error_class_omitted() -> None:
    """error_class must NOT appear in the payload when it is None."""
    captured: list[dict[str, object]] = []

    def h(req: httpx.Request) -> httpx.Response:
        captured.append(json.loads(req.content))
        return httpx.Response(200, json={"tx_id": "tx-3", "status": "broadcast_accepted"})

    with _client(h) as fwd:
        fwd.report_broadcast_result("tx-3", "0xhash", "accepted")

    assert "error_class" not in captured[0]


def test_report_broadcast_result_terminal_on_409() -> None:
    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(409, json={"error": "tx_hash_mismatch", "message": "bad"})

    with _client(h) as fwd:
        with pytest.raises(FwdTerminalError) as ei:
            fwd.report_broadcast_result("tx-x", "0xh", "accepted")
    assert ei.value.status == 409


def test_report_broadcast_result_transport_error_retryable() -> None:
    with _raising_client(httpx.ConnectError("down")) as fwd:
        with pytest.raises(FwdRetryableError):
            fwd.report_broadcast_result("tx-x", "0xh", "accepted")


# ---- report_receipt ----


def test_report_receipt_mined_success() -> None:
    captured: list[dict[str, object]] = []

    def h(req: httpx.Request) -> httpx.Response:
        captured.append(json.loads(req.content))
        return httpx.Response(200, json={"tx_id": "tx-r", "status": "receipt_recorded"})

    with _client(h) as fwd:
        r = fwd.report_receipt("tx-r", "0xhash", "mined_success", 999)

    assert r.tx_id == "tx-r"
    assert isinstance(r, ReceiptResponse)
    body = captured[0]
    assert body["outcome"] == "mined_success"
    assert body["block_number"] == 999
    assert body["tx_hash"] == "0xhash"


def test_report_receipt_mined_reverted() -> None:
    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"tx_id": "tx-rv", "status": "receipt_recorded"})

    with _client(h) as fwd:
        r = fwd.report_receipt("tx-rv", "0xhash", "mined_reverted", 1000)
    assert r.tx_id == "tx-rv"


def test_report_receipt_transport_error_retryable() -> None:
    with _raising_client(httpx.ConnectError("down")) as fwd:
        with pytest.raises(FwdRetryableError):
            fwd.report_receipt("tx-x", "0xh", "mined_success", 1)


# ---- idempotency key ----


def test_idempotency_key_deterministic() -> None:
    a = make_idempotency_key("flare", "1", "0xabc", "100")
    b = make_idempotency_key("flare", "1", "0xabc", "100")
    assert a == b


def test_idempotency_key_distinct_on_different_parts() -> None:
    a = make_idempotency_key("flare", "1", "0xabc", "100")
    b = make_idempotency_key("flare", "1", "0xabc", "101")  # different epoch
    c = make_idempotency_key("songbird", "1", "0xabc", "100")  # different network
    assert a != b
    assert a != c


def test_idempotency_key_bounded() -> None:
    k = make_idempotency_key("flare", "1", "0x" + "a" * 40, "100")
    assert len(k) <= 128


def test_idempotency_key_retry_none_is_stable() -> None:
    """retry=None must produce the same key as no retry argument."""
    base = make_idempotency_key("flare", "1", "0xabc", "100")
    with_none = make_idempotency_key("flare", "1", "0xabc", "100", retry=None)
    assert base == with_none


def test_idempotency_key_retry_discriminator() -> None:
    """Same parts + same retry = same key (dedup). Different retry = fresh key."""
    base = make_idempotency_key("flare", "1", "0xabc", "100")
    r1a = make_idempotency_key("flare", "1", "0xabc", "100", retry="op-1")
    r1b = make_idempotency_key("flare", "1", "0xabc", "100", retry="op-1")
    r2 = make_idempotency_key("flare", "1", "0xabc", "100", retry="op-2")
    assert r1a == r1b  # same retry = same key (crash-rerun dedup)
    assert r1a != base  # retry discriminates from no-retry
    assert r1a != r2  # different retry value = fresh key
    assert len(r2) <= 128


def test_idempotency_key_golden_parity() -> None:
    """Byte-identical to the golden vectors pinned in go/idempotency_test.go.

    Cross-language parity gate (CLAUDE.md invariant 3): a Go consumer and a
    Python consumer (clif) dedup the same logical attempt at fwd only if both
    languages emit the same key for the same (parts, retry). These literals are
    duplicated verbatim in both suites so a drift in EITHER language fails its
    own tests.
    """
    assert make_idempotency_key("clif", "songbird", "2", "404") == "clif:songbird:2:404-c881a5983a4d9488"
    assert (
        make_idempotency_key("clif", "songbird", "2", "404", retry="r2")
        == "clif:songbird:2:404:retry=r2-09dc2f33c4c26a1c"
    )
    assert make_idempotency_key("a", "b") == "a:b-6783a31eabf68ccc"
    assert make_idempotency_key("with space", "x") == "with_space:x-3dd10fc2bfebffda"
    assert make_idempotency_key("only") == "only-f905b19542ed08c9"
    long_parts = [f"seg{i:02d}" for i in range(30)]
    assert make_idempotency_key(*long_parts) == (
        "seg00:seg01:seg02:seg03:seg04:seg05:seg06:seg07:seg08:seg09:"
        "seg10:seg11:seg12:se-193f16a1387c3d78"
    )
