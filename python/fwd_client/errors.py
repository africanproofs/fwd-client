"""fwd error hierarchy and response-to-exception helper.

fwd v1.1.0a9+ status taxonomy:
  Terminal (never retry): 400, 401, 403, 404, 422, and most 409s.
  Retryable (backoff and retry): 503, any httpx transport error, and 409
    idempotent_replay (see below).
  Note: 502 is GONE — fwd no longer does any RPC; broadcast/receipt errors
    are the caller's own responsibility. Unmapped statuses fail closed (terminal).

409 dispatch (FWD-CLIENT-ERRORTAX-007):
  Not all 409s are the same — they are split by error_code:
  - "idempotent_replay": the idempotency key was already used and fwd is
    replaying the cached response.  This is NOT an error — treat as retryable
    so callers transparently receive the replay.
  - "nonce_not_initialized": the operator must run `clifwd nonce-init` for
    this (wallet, chain) before the caller can use it.  Terminal — retrying
    without operator action will never succeed.
  - "illegal_transition": the tx is in a state that makes this operation
    invalid (e.g. trying to broadcast-result a tx that is already confirmed).
    Terminal — a true conflict.
  - "hash_mismatch": the tx hash in the report does not match what fwd signed.
    Terminal — data integrity violation.
  - any other 409: terminal (fail closed).
"""

from __future__ import annotations

import httpx  # noqa: TC002 — used at runtime (response.status_code, type(exc).__name__, .json())

# fwd v1.1.0a9+: 502 removed (fwd does no RPC).
# 409 is split by error_code (see module docstring); excluded from blanket terminal set.
_TERMINAL_STATUSES: frozenset[int] = frozenset({400, 401, 403, 404, 422})
_RETRYABLE_STATUSES: frozenset[int] = frozenset({503})

# 409 sub-taxonomy by error_code.
_409_RETRYABLE: frozenset[str] = frozenset({"idempotent_replay"})
# All other 409 error_codes are terminal; this set documents the known ones explicitly.
_409_TERMINAL: frozenset[str] = frozenset({
    "nonce_not_initialized",   # operator must run clifwd nonce-init
    "illegal_transition",      # true state-machine conflict
    "hash_mismatch",           # data integrity violation
})

# Synthetic status for transport errors (no HTTP response received).
_TRANSPORT_ERROR_STATUS: int = 0


class FwdError(RuntimeError):
    """Base class for all fwd client errors."""

    def __init__(self, status: int, error_code: str, message: str) -> None:
        super().__init__(f"fwd {status} {error_code}: {message}")
        self.status = status
        self.error_code = error_code
        self.message = message


class FwdTerminalError(FwdError):
    """Do not retry — auth/policy/wallet/bad-request/sealed-master failure."""


class FwdRetryableError(FwdError):
    """May retry after backoff — fwd is down, restarting, or overloaded."""


def raise_for_fwd_error(response: httpx.Response) -> None:
    """Inspect *response* and raise the appropriate FwdError subclass.

    200 responses pass through silently. Every other status is classified as
    terminal or retryable per the fwd v1.1.0a9+ taxonomy; unmapped statuses
    fail closed (terminal) to prevent accidental retry on unknown error shapes.

    409 is split by error_code (FWD-CLIENT-ERRORTAX-007):
    - idempotent_replay → FwdRetryableError (transparent replay, not a fault)
    - all others → FwdTerminalError (operator action needed)
    """
    if response.status_code == 200:
        return
    try:
        body = response.json()
        err = str(body.get("error", "unknown"))
        msg = str(body.get("message", response.text))
    except ValueError:
        err, msg = "unknown", response.text
    if response.status_code in _RETRYABLE_STATUSES:
        raise FwdRetryableError(response.status_code, err, msg)
    if response.status_code == 409:
        # Split 409 by error_code: idempotent_replay is retryable; all others terminal.
        if err in _409_RETRYABLE:
            raise FwdRetryableError(response.status_code, err, msg)
        raise FwdTerminalError(response.status_code, err, msg)
    # Terminal: both explicit set AND unmapped catch-all (fail closed).
    raise FwdTerminalError(response.status_code, err, msg)


def transport_retryable(exc: httpx.RequestError) -> FwdRetryableError:
    """Wrap an httpx transport error as a FwdRetryableError.

    A down or restarting fwd must degrade the caller gracefully, never crash it.
    """
    return FwdRetryableError(
        _TRANSPORT_ERROR_STATUS,
        "transport_error",
        f"{type(exc).__name__}: {exc}",
    )
