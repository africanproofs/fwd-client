# fwd-client

The one canonical keyless client for the [fwd](https://github.com/africanproofs/fwd) signing
daemon — pure transport: **no keys, no crypto, no broadcast**. fwd signs and returns a raw
transaction blob; the caller broadcasts it and reports the outcome back.

Two implementations of the same fwd contract, in dedicated folders:

- **[`python/`](python/)** — Python 3.12 package (`fwd_client`), httpx + pydantic. See [`python/README.md`](python/README.md).
- **[`go/`](go/)** — Go module (`github.com/africanproofs/fwd-client/go`), stdlib only. See [`go/README.md`](go/README.md).

Both track the same fwd API and expose an idempotency-key helper
(`make_idempotency_key` / `MakeIdempotencyKey`) that produces **byte-identical** keys, so a
Go consumer and a Python consumer dedup the same logical attempt at fwd.

## Keyless by design

`fwd-client` holds no private keys, performs no signing, and has no crypto dependencies.
All signing happens inside the fwd daemon; broadcasting and chain reads stay in each consumer.

## Contract

fwd owns the HTTP contract (`sign-transaction`, `sign-fsp-message`, the
`broadcast-result`/`receipt` report-back loop, `transactions/{id}`, `healthz`); this repo is
its client mirror. Error taxonomy: **terminal** = 400/401/403/404/422 and most 409s (and any
unmapped status — fail closed); **retryable** = 503, transport errors, and the 409
`idempotent_replay` code.

## Releases

- Python → tag `vX.Y.Z`; consumed as a git dep with `subdirectory = "python"`.
- Go → tag `go/vX.Y.Z` (Go subdir-module convention); consumers use `@vX.Y.Z`.

Current: **v0.1.3** (Python + Go lockstep — tags `v0.1.3` + `go/v0.1.3`). v0.1.3 makes Python `health()` raise `FwdRetryableError` on a 503 (taxonomy parity with Go) + adds GitHub Actions CI; v0.1.2 fixed the Python error-envelope parser to read the nested `detail.error`.
