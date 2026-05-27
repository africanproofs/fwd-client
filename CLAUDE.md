# fwd-client — shared keyless client library for the `fwd` signing daemon

> The one canonical way AP consumers interface to **fwd**. Pure HTTP transport;
> holds and touches **no keys and no crypto**. Extracted from clif's mature
> `fwd_client.py` (2026-05-27) so clif and future clients (FSP signing-tool,
> apregister Coston2, apcli, fics write paths, demo/example clients) share **one**
> implementation of the fwd contract instead of each reinventing it.

## What it is

A small Python package (`fwd_client`) wrapping the fwd v1.1.0a9+ **zero-egress /
sign-only** HTTP API: `sign-transaction`, `sign-fsp-message`, the report-back loop
(`broadcast-result`, `receipt`), `transactions/{id}`, `healthz`. It encodes the
contract once: the request/response models, the terminal-vs-retryable error
taxonomy, and a deterministic idempotency-key helper.

## THE invariant — keyless, no crypto

Inviolable, inherited from clif's keyless rule and fwd's custody model. This package
depends on **`httpx` + `pydantic` only**. It must NEVER import or depend on
`eth-account`, `eth-keys`, `eth-abi`, `eth-hash`, `web3`, `pycryptodome`,
`coincurve`, or any signing/crypto library. It is *transport to fwd* — nothing more.
Signing happens in fwd; broadcasting, chain reads, and calldata/Merkle building stay
in each consumer. Any change that adds a crypto dep is a regression — STOP.

## Public API

`FwdClient` (sync) + `FwdError` / `FwdTerminalError` / `FwdRetryableError` +
`raise_for_fwd_error` + `make_idempotency_key` + the request/response models
(`SignTransaction*`, `BroadcastResult*`, `Receipt*`, `SignFspMessageResponse`,
`TxStatus`, `Health`). Error taxonomy: **terminal** = 400/401/403/404/409/422 (and
any unmapped status — fail closed); **retryable** = 503 + httpx transport errors;
**502 no longer occurs** (fwd does no RPC).

## Contract lockstep (why this exists)

fwd owns the contract; this package is its client mirror. When fwd's API changes
(as in the v1.1.0a9 zero-egress migration: `sign-and-send` → `sign-transaction` +
client-side broadcast + report-back), it is a **one-package bump here** rather than
a hunt across every consumer. Version it forward; pin consumers to a tag.

## Stack

Python 3.12 · Poetry · httpx · Pydantic v2. `fwd_client/`: `client`, `errors`,
`models`, `idempotency`. `tests/` (httpx-mocked; ported from clif). ruff + mypy
clean.

## Consumed by

clif (FTSO claims + FSP signing) and future fwd consumers. The idempotency-key
*content* (e.g. clif's `network/claim_type/epoch` composition) stays in each
consumer; this lib provides the generic `make_idempotency_key(*parts)`.
