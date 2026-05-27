"""FSP: sign_fsp_message cross-field validation, HTTP contract, error mapping.

Adapted from clif tests/test_fsp_fwd_client.py for the generic fwd-client package.
"""

from __future__ import annotations

import httpx
import pytest
from fwd_client import FwdClient, FwdRetryableError, FwdTerminalError
from fwd_client.models import SignFspMessageResponse

FAKE_RESPONSE = {
    "message_hash": "0xb7e97e6b4b2c7cd5fb9b51a86ad7eae441872b770b5953443024cb1e0bc6f67d",
    "v": 27,
    "r": "0x9938afc59dae94cb20e0c5982e00c6a88afc01f6ff8c058024f999857a32e785",
    "s": "0x1e926390fbdece399aa1c56dbcbc66d128d43fba246b9459d5018d0c2de9b4b5",
    "signature": "0x" + "ab" * 65,
}

REWARDS_HASH = "0x" + "ab" * 32


def _client(handler: object) -> FwdClient:
    fwd = FwdClient("http://fwd:8080", "fwd_test_token")
    fwd._client = httpx.Client(transport=httpx.MockTransport(handler))  # type: ignore[arg-type]
    return fwd


# ---- cross-field validation fires BEFORE any HTTP ----


def test_uptime_rejects_chain_id() -> None:
    with _client(lambda _: httpx.Response(200, json=FAKE_RESPONSE)) as fwd:
        with pytest.raises(ValueError, match="all be None"):
            fwd.sign_fsp_message("w", "UPTIME", 0, chain_id=114)


def test_uptime_rejects_no_of_claims() -> None:
    with _client(lambda _: httpx.Response(200, json=FAKE_RESPONSE)) as fwd:
        with pytest.raises(ValueError, match="all be None"):
            fwd.sign_fsp_message("w", "UPTIME", 0, no_of_weight_based_claims=5)


def test_uptime_rejects_rewards_hash() -> None:
    with _client(lambda _: httpx.Response(200, json=FAKE_RESPONSE)) as fwd:
        with pytest.raises(ValueError, match="all be None"):
            fwd.sign_fsp_message("w", "UPTIME", 0, rewards_hash=REWARDS_HASH)


def test_reward_distribution_requires_all_three() -> None:
    with _client(lambda _: httpx.Response(200, json=FAKE_RESPONSE)) as fwd:
        with pytest.raises(ValueError, match="all required"):
            fwd.sign_fsp_message("w", "REWARD_DISTRIBUTION", 3)


def test_reward_distribution_requires_rewards_hash_present() -> None:
    with _client(lambda _: httpx.Response(200, json=FAKE_RESPONSE)) as fwd:
        with pytest.raises(ValueError, match="all required"):
            fwd.sign_fsp_message(
                "w", "REWARD_DISTRIBUTION", 3, chain_id=114, no_of_weight_based_claims=56
            )


def test_reward_distribution_bad_rewards_hash_format() -> None:
    with _client(lambda _: httpx.Response(200, json=FAKE_RESPONSE)) as fwd:
        with pytest.raises(ValueError, match="rewards_hash must match"):
            fwd.sign_fsp_message(
                "w",
                "REWARD_DISTRIBUTION",
                3,
                chain_id=114,
                no_of_weight_based_claims=56,
                rewards_hash="not-a-hash",
            )


def test_unknown_message_type_raises() -> None:
    with _client(lambda _: httpx.Response(200, json=FAKE_RESPONSE)) as fwd:
        with pytest.raises(ValueError, match="Unknown message_type"):
            fwd.sign_fsp_message("w", "BOGUS", 0)


def test_cross_field_check_fires_before_http() -> None:
    """No HTTP must be made when cross-field validation fails."""
    calls: list[int] = []

    def h(req: httpx.Request) -> httpx.Response:
        calls.append(1)
        return httpx.Response(200, json=FAKE_RESPONSE)

    with _client(h) as fwd:
        with pytest.raises(ValueError):
            fwd.sign_fsp_message("w", "UPTIME", 0, chain_id=99)
    assert calls == [], "HTTP was called before the cross-field check fired"


# ---- happy-path: UPTIME ----


def test_sign_fsp_uptime_success() -> None:
    import json

    def h(req: httpx.Request) -> httpx.Response:
        payload = json.loads(req.content)
        assert payload["message_type"] == "UPTIME"
        assert payload["reward_epoch_id"] == 0
        # UPTIME must NOT include reward-distribution fields.
        assert "chain_id" not in payload
        assert "no_of_weight_based_claims" not in payload
        assert "rewards_hash" not in payload
        # Must NOT include a raw digest field.
        assert "digest" not in payload
        assert "hash" not in payload
        return httpx.Response(200, json=FAKE_RESPONSE)

    with _client(h) as fwd:
        r = fwd.sign_fsp_message("signing-wallet", "UPTIME", 0)
    assert r.v == 27
    assert r.message_hash == FAKE_RESPONSE["message_hash"]
    assert isinstance(r, SignFspMessageResponse)


# ---- happy-path: REWARD_DISTRIBUTION ----


def test_sign_fsp_rewards_success() -> None:
    import json

    def h(req: httpx.Request) -> httpx.Response:
        payload = json.loads(req.content)
        assert payload["message_type"] == "REWARD_DISTRIBUTION"
        assert payload["chain_id"] == 114
        assert payload["no_of_weight_based_claims"] == 56
        assert payload["rewards_hash"] == REWARDS_HASH
        # Must NOT include a raw digest field.
        assert "digest" not in payload
        return httpx.Response(200, json=FAKE_RESPONSE)

    with _client(h) as fwd:
        r = fwd.sign_fsp_message(
            "signing-wallet",
            "REWARD_DISTRIBUTION",
            3,
            chain_id=114,
            no_of_weight_based_claims=56,
            rewards_hash=REWARDS_HASH,
        )
    assert r.v == 27


# ---- idempotency key header ----


def test_idempotency_key_header_sent() -> None:
    def h(req: httpx.Request) -> httpx.Response:
        assert req.headers.get("Idempotency-Key") == "test-key-123"
        return httpx.Response(200, json=FAKE_RESPONSE)

    with _client(h) as fwd:
        fwd.sign_fsp_message("w", "UPTIME", 0, idempotency_key="test-key-123")


def test_no_idempotency_key_no_header() -> None:
    def h(req: httpx.Request) -> httpx.Response:
        assert "Idempotency-Key" not in req.headers
        return httpx.Response(200, json=FAKE_RESPONSE)

    with _client(h) as fwd:
        fwd.sign_fsp_message("w", "UPTIME", 0)


# ---- error status mapping ----


@pytest.mark.parametrize(
    "code,err_code",
    [
        (403, "policy_denied"),
        (404, "wallet_not_found"),
        (422, "transaction_rejected"),
    ],
)
def test_terminal_statuses_fsp(code: int, err_code: str) -> None:
    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(code, json={"error": err_code, "message": "no"})

    with _client(h) as fwd:
        with pytest.raises(FwdTerminalError) as ei:
            fwd.sign_fsp_message("w", "UPTIME", 0)
    assert ei.value.status == code


@pytest.mark.parametrize(
    "code,err_code",
    [
        (503, "vault_unavailable"),
    ],
)
def test_retryable_statuses_fsp(code: int, err_code: str) -> None:
    """fwd v1.1.0a9+: 502 is GONE; only 503 is retryable."""

    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(code, json={"error": err_code, "message": "down"})

    with _client(h) as fwd:
        with pytest.raises(FwdRetryableError):
            fwd.sign_fsp_message("w", "UPTIME", 0)


def test_502_is_now_terminal_for_sign_fsp_message() -> None:
    """502 is GONE in fwd v1.1.0a9+. Falls through to the terminal catch-all."""

    def h(_req: httpx.Request) -> httpx.Response:
        return httpx.Response(502, json={"error": "old_rpc_unreachable", "message": "gone"})

    with _client(h) as fwd:
        with pytest.raises(FwdTerminalError):
            fwd.sign_fsp_message("w", "UPTIME", 0)


def test_transport_error_is_retryable_fsp() -> None:
    def h(req: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("down")

    fwd = FwdClient("http://fwd:8080", "fwd_test_token")
    fwd._client = httpx.Client(transport=httpx.MockTransport(h))
    with pytest.raises(FwdRetryableError) as ei:
        fwd.sign_fsp_message("w", "UPTIME", 0)
    assert ei.value.error_code == "transport_error"
    fwd.close()
