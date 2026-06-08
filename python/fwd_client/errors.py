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
    "tx_hash_mismatch",        # report hash does not match what fwd signed
    "idempotency_conflict",    # same key, different body (a caller may choose to treat as already-submitted)
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


def _parse_envelope(response: httpx.Response) -> tuple[str, str]:
    """Extract (error_code, message) from a fwd error body — byte-for-byte taxonomy
    parity with the Go ``parseEnvelope``.

    fwd renders errors via FastAPI's HTTPException, so the real wire shape is
    NESTED: ``{"detail": {"error": "<code>", "message": "<msg>"}}``. Reading the
    top level alone (the pre-v0.1.2 bug) always saw ``"unknown"``. This handles:
      - the nested detail-object (the real fwd error wire),
      - a detail-STRING shape (e.g. the 503 ``/healthz`` degraded body),
      - a FastAPI auto-validation detail-LIST 422 (falls back to "unknown"),
      - a flat ``{"error","message"}`` shape (defensive),
    and falls back to ("unknown", raw text) for non-JSON / shapeless bodies.
    """
    try:
        body = response.json()
    except ValueError:
        return "unknown", response.text
    code, msg = "", ""
    if isinstance(body, dict):
        code = str(body.get("error") or "")
        msg = str(body.get("message") or "")
        detail = body.get("detail")
        if isinstance(detail, dict):
            d_err = str(detail.get("error") or "")
            if d_err:
                code, msg = d_err, str(detail.get("message") or "")
        elif isinstance(detail, str) and detail and not msg:
            msg = detail
    if not code:
        code = "unknown"
    if not msg:
        msg = response.text
    return code, msg


def raise_for_fwd_error(response: httpx.Response) -> None:
    """Inspect *response* and raise the appropriate FwdError subclass.

    200 responses pass through silently. Every other status is classified as
    terminal or retryable per the fwd v1.1.0a9+ taxonomy; unmapped statuses
    fail closed (terminal) to prevent accidental retry on unknown error shapes.

    409 is split by error_code (FWD-CLIENT-ERRORTAX-007):
    - idempotent_replay → FwdRetryableError (transparent replay, not a fault)
    - all others → FwdTerminalError (operator action needed)

    error_code is parsed from the NESTED FastAPI envelope (``detail.error``), so
    callers may reliably branch on it (e.g. clif treats ``idempotency_conflict``
    as already-submitted). Before v0.1.2 it was always ``"unknown"``.
    """
    if response.status_code == 200:
        return
    err, msg = _parse_envelope(response)
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
