# fwd-client

Keyless HTTP client for the [fwd](https://gitlab.com/proofs.africa/fwd) signing daemon.

Tracks the **fwd v1.1.0a9+ zero-egress / sign-only API**: fwd signs and returns
a raw transaction blob; the caller broadcasts and reports back.  This library is
pure HTTP transport — no crypto, no calldata, no broadcast logic.

## Install

```
pip install fwd-client
```

Or with Poetry:

```
poetry add fwd-client
```

**No crypto dependencies.** The only runtime deps are `httpx` and `pydantic` (v2).

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

## Go client

A Go port of this library — same fwd contract, same keyless boundary — lives in
[`go/`](go/) (`module gitlab.com/proofs.africa/fwd-client/go`, stdlib only). Its
`MakeIdempotencyKey` is byte-identical to the Python helper, so Go and Python
consumers dedup the same logical attempt at fwd. See [`go/README.md`](go/README.md).
The two clients are released in lockstep from this repo.

## Keyless by design

`fwd-client` holds no private keys, performs no signing, and has no crypto
dependencies.  All signing happens inside the fwd daemon.

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
| 400, 401, 403, 404, 409, 422 | `FwdTerminalError` |
| 503 | `FwdRetryableError` |
| transport error (no response) | `FwdRetryableError` |
| any other status | `FwdTerminalError` (fail closed) |

Note: 502 is gone — fwd no longer performs any RPC calls.
