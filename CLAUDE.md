# fwd-client — keyless client library for the `fwd` signing daemon

> Two parallel implementations of **one** fwd contract — Python (`python/`) and Go
> (`go/`), peers (neither is "the port"). The canonical, keyless way each language's
> consumers interface to **fwd**: pure HTTP transport that holds and touches **no keys
> and no crypto**. Canonical home: `github.com/africanproofs/fwd-client`. (Python was
> extracted from clif's mature `fwd_client.py`, 2026-05-27; the Go port followed.)

## Layout

- **`python/`** — the `fwd_client` package (`httpx` + `pydantic`). `python/fwd_client/`
  (`client`, `errors`, `models`, `idempotency`), `python/tests/` (httpx-mocked), `pyproject.toml`,
  `poetry.lock`, `README.md`.
- **`go/`** — module `github.com/africanproofs/fwd-client/go` (**stdlib only**). `client.go`,
  `errors.go`, `models.go`, `idempotency.go`, `*_test.go`, `README.md`.
- **root** — overview `README.md`, this `CLAUDE.md`, `.github/workflows/ci.yml` (the effective CI,
  one job per language; a legacy `.gitlab-ci.yml` is inert on the github-only remote).

The two folders are equal and self-contained. The same fwd contract, mirrored in each idiom.

## Invariants (hold in both languages)

1. **Keyless, no crypto.** This library is *transport to fwd* — nothing more. Signing happens
   in fwd; broadcasting, chain reads, and calldata/Merkle building stay in each consumer.
   Python depends on **`httpx` + `pydantic` only** — it must NEVER import `eth-account`,
   `eth-keys`, `eth-abi`, `eth-hash`, `web3`, `pycryptodome`, or `coincurve`. Go is **stdlib
   only** — never `go-ethereum` or any crypto package. Adding a crypto dependency is a
   regression — STOP.

2. **Pure transport.** No broadcast, no `eth_*` calls, no calldata/Merkle building, no chain
   reads. The client signs via fwd and reports back; the consumer broadcasts.

3. **Idempotency keys are byte-identical across languages.** `make_idempotency_key` (Python)
   and `MakeIdempotencyKey` (Go) MUST produce the same string for the same `(parts, retry)`:
   `sha256` of `":".join(parts)` (plus `:retry=<r>` when a retry discriminator is set) →
   `<first-80-chars, spaces→_>-<digest[:16]>`, capped at 128. A Go consumer and a Python
   consumer (clif) dedup the *same logical attempt* at fwd only because of this. **Gate:**
   identical golden vectors are pinned in BOTH `go/idempotency_test.go` and
   `python/tests/test_fwd_client.py`. Changing the algorithm in either language requires
   re-verifying both emit identical output and updating both golden sets.

4. **Error taxonomy mirrors fwd; status-driven.** Terminal: 400/401/403/404/422 and any
   unmapped status (fail closed). Retryable: 503 and transport/network errors. **409 is split
   by error code:** `idempotent_replay` → retryable (a transparent cached replay); every other
   409 → terminal. 502 never occurs (fwd does no RPC). Idiom differs: Python raises a class
   hierarchy (`FwdError` → `FwdTerminalError` / `FwdRetryableError`); Go returns a single
   `Error{Status, Code, Message, Retryable}` with `IsRetryable` / `IsTerminal` helpers.

5. **fwd's error body is nested** under `detail`: `{"detail": {"error": "<code>", "message":
   "<msg>"}}` (FastAPI's default `HTTPException` rendering — confirmed by fwd's own tests).
   Parse `detail.error` first and fall back to the historical top-level `error`
   shape only for compatibility. Python `v0.1.2` and Go both surface the real
   fwd error code; a client that reads only top-level `body["error"]` regresses
   to `"unknown"` and breaks consumers that need a narrow code-specific recovery.

6. **Contract lockstep.** fwd owns the HTTP contract; both clients mirror it. A fwd contract
   change (endpoint, request/response model, error taxonomy, or idempotency algorithm) updates
   **both** `python/` and `go/` in the same release. The API surface, models, taxonomy, and
   idempotency algorithm are identical across languages; only the idioms differ.

## Public API

Both clients cover the same fwd endpoints: `POST /v1/sign-transaction`,
`POST /v1/sign-fsp-message`, the report-back loop (`POST /v1/transactions/{id}/broadcast-result`,
`/receipt`), `GET /v1/transactions/{id}`, `GET /healthz`. `sign-replacement` is **deliberately
not exposed** in either client — add it only when a consumer needs stuck-tx replacement.

- **Python** (`from fwd_client import …`): `FwdClient` + `FwdError` / `FwdTerminalError` /
  `FwdRetryableError` + `raise_for_fwd_error` + `make_idempotency_key` + the models
  (`SignTransaction*`, `BroadcastResult*`, `Receipt*`, `SignFspMessageResponse`, `TxStatus`,
  `Health`). `__version__` mirrors the package version.
- **Go** (`import fwdclient "github.com/africanproofs/fwd-client/go"`): `New(baseURL, token,
  …Option)` (`WithTimeout` / `WithHTTPClient`) + `Client.{SignTransaction, SignFspMessage,
  ReportBroadcastResult, ReportReceipt, GetTransaction, Health}` + `IsRetryable` / `IsTerminal`
  + `Error` + `MakeIdempotencyKey` + the same models.

## Consumption

- **Python** (Poetry 1.2+ — the package lives in the `python/` subdir):
  ```toml
  fwd-client = {git = "https://github.com/africanproofs/fwd-client.git", tag = "vX.Y.Z", subdirectory = "python"}
  ```
  Imports stay `from fwd_client import …`. clif is today's only consumer.
- **Go**: `go get github.com/africanproofs/fwd-client/go@vX.Y.Z` — the git tag is `go/vX.Y.Z`
  (Go subdir-module convention) and Go maps the plain `@vX.Y.Z` version to it.

## Release & versioning

- Release the two languages in **lockstep on the same number**: Python tag `vX.Y.Z` + Go tag
  `go/vX.Y.Z`, at the same commit.
- **Bump `python/pyproject.toml::version` AND `python/fwd_client/__init__.py::__version__`
  together** — they are two sources of one number and silently drift if you bump only one.
- **Do not move a published tag.** A moved tag is invisible to a consumer's
  `poetry lock --no-update` (it keeps the already-locked commit) — the consumer must run
  `poetry update fwd-client` (or clear Poetry's git cache) to repoint. Prefer cutting a new
  patch over moving a tag.
- Current: **v0.1.3** (Python + Go in lockstep: tags `v0.1.3` + `go/v0.1.3`). v0.1.3 makes Python
  `health()` use the fwd taxonomy (a degraded daemon's 503 now raises `FwdRetryableError`, not a raw
  `httpx.HTTPStatusError` — parity with Go `Health()`) and adds the effective GitHub Actions CI. The
  prior **v0.1.2** fixed the Python error-envelope parser to read the nested `detail.error`
  (`FwdError.error_code` was always `"unknown"`; Go already parsed it) with a golden Python↔Go parity
  test; `v0.1.0` / `go/v0.1.0` are the legacy root-layout release; old pins still resolve.

## CI

`.github/workflows/ci.yml` (GitHub Actions — the repo is github-hosted, so the legacy `.gitlab-ci.yml`
is inert on the remote): a **`python`** job (`poetry install` + `ruff` + `pytest`, `working-directory:
python`) and a **`go`** job (`gofmt -l` + `go vet` + `go test`, `working-directory: go`). One job per
language, run on every push + PR.
