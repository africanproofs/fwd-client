# fwd-client (Python)

Keyless HTTP client for the [fwd](https://github.com/africanproofs/fwd) signing daemon.

Tracks the **fwd v1.1.0a9+ zero-egress / sign-only API**: fwd signs and returns a raw
transaction blob; the caller broadcasts and reports back. Pure HTTP transport — no crypto,
no calldata, no broadcast logic. Runtime deps: `httpx` + `pydantic` (v2) only.

## Install

This package lives in the `python/` subdir of the fwd-client repo (the Go port is in `go/`),
so consume it as a git dependency with `subdirectory`:

```toml
# pyproject.toml (Poetry 1.2+)
fwd-client = {git = "https://github.com/africanproofs/fwd-client.git", tag = "v0.1.1", subdirectory = "python"}
```

## Usage

```python
from fwd_client import FwdClient, FwdRetryableError, FwdTerminalError, make_idempotency_key

idem_key = make_idempotency_key("my-app", "claim", "flare", "1234")

with FwdClient("http://fwd:8080", caller_token="tok_...") as fwd:
    # 1. Sign (fwd allocates nonce; you supply gas params)
    signed = fwd.sign_transaction(
        wallet="my-wallet",
        chain=14,
        to="0x...",
        gas=200_000,
        max_fee_per_gas=50_000_000_000,
        max_priority_fee_per_gas=1_000_000_000,
        data="0xcafe...",
        idempotency_key=idem_key,
    )

    # 2. Broadcast yourself (signed.signed_raw_tx -> eth_sendRawTransaction)
    tx_hash = my_rpc.send_raw_transaction(signed.signed_raw_tx)

    # 3. Report broadcast result to fwd
    fwd.report_broadcast_result(signed.tx_id, tx_hash, "accepted")

    # 4. After mining, report the receipt
    fwd.report_receipt(signed.tx_id, tx_hash, "mined_success", block_number=12345678)
```

## Keyless by design

`fwd-client` holds no private keys, performs no signing, and has no crypto dependencies.
All signing happens inside the fwd daemon.

## Error handling

```python
from fwd_client import FwdTerminalError, FwdRetryableError

try:
    signed = fwd.sign_transaction(...)
except FwdRetryableError:
    # fwd is down or restarting — back off and retry
    ...
except FwdTerminalError:
    # auth/policy/wallet failure — do not retry; alert operator
    ...
```

Status taxonomy (fwd v1.1.0a9+):

| Status | Class |
|--------|-------|
| 400, 401, 403, 404, 422 (+ most 409) | `FwdTerminalError` |
| 503 | `FwdRetryableError` |
| transport error (no response) | `FwdRetryableError` |
| 409 `idempotent_replay` | `FwdRetryableError` |
| any other status | `FwdTerminalError` (fail closed) |

Note: 502 is gone — fwd no longer performs any RPC calls.

## Develop

```sh
cd python
poetry install
poetry run pytest -q
poetry run ruff check .
```
