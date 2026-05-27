"""fwd error hierarchy and response-to-exception helper.

fwd v1.1.0a9+ status taxonomy:
  Terminal (never retry): 400, 401, 403, 404, 409, 422.
  Retryable (backoff and retry): 503, and any httpx transport error.
  Note: 502 is GONE — fwd no longer does any RPC; broadcast/receipt errors
    are the caller's own responsibility. Unmapped statuses fail closed (terminal).
"""

from __future__ import annotations

import httpx  # noqa: TC002 — used at runtime (response.status_code, type(exc).__name__, .json())

# fwd v1.1.0a9+: 502 removed (fwd does no RPC); 409 (nonce_not_initialized) is terminal.
_TERMINAL_STATUSES: frozenset[int] = frozenset({400, 401, 403, 404, 409, 422})
_RETRYABLE_STATUSES: frozenset[int] = frozenset({503})

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
