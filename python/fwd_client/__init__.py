"""fwd-client — keyless HTTP client for the fwd signing daemon.

Public API::

    from fwd_client import FwdClient, FwdError, FwdTerminalError, FwdRetryableError
    from fwd_client import make_idempotency_key
    from fwd_client.models import SignTransactionResponse, ...
"""

from __future__ import annotations

__version__ = "0.1.2"

from fwd_client.client import FwdClient
from fwd_client.errors import FwdError, FwdRetryableError, FwdTerminalError, raise_for_fwd_error
from fwd_client.idempotency import make_idempotency_key
from fwd_client.models import (
    BroadcastResultRequest,
    BroadcastResultResponse,
    Health,
    ReceiptRequest,
    ReceiptResponse,
    SignFspMessageResponse,
    SignTransactionRequest,
    SignTransactionResponse,
    TxStatus,
)

__all__ = [
    "__version__",
    # Client
    "FwdClient",
    # Errors
    "FwdError",
    "FwdTerminalError",
    "FwdRetryableError",
    "raise_for_fwd_error",
    # Idempotency
    "make_idempotency_key",
    # Models
    "BroadcastResultRequest",
    "BroadcastResultResponse",
    "Health",
    "ReceiptRequest",
    "ReceiptResponse",
    "SignFspMessageResponse",
    "SignTransactionRequest",
    "SignTransactionResponse",
    "TxStatus",
]
