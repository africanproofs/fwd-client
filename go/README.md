# fwd-client (Go)

Keyless Go transport client for the [fwd](https://github.com/africanproofs/fwd)
signing daemon — the Go port of the Python `fwd-client`, speaking the same
**fwd v1.1.0a9+ zero-egress / sign-only** HTTP contract.

Pure transport: **no crypto, no calldata, no keys, no broadcast.** fwd signs and
returns a raw transaction blob; the caller broadcasts it itself (e.g. via
go-ethereum `eth_sendRawTransaction`) and reports the outcome back. Stdlib only —
no third-party dependencies.

## Install

```sh
go get github.com/africanproofs/fwd-client/go@go/v0.1.0
```

> This is a Go module in the `go/` subdir of the `fwd-client` repo, so version
> tags are prefixed: `go/vX.Y.Z`. `@latest` resolves the newest `go/v*` tag.

```go
import fwdclient "github.com/africanproofs/fwd-client/go"
```

## Usage

```go
ctx := context.Background()
c := fwdclient.New("http://fwd:8080", callerToken) // fwdclient.WithTimeout(...) optional

// 1) Sign (fwd allocates the nonce; you supply gas + EIP-1559 fees).
idem := fwdclient.MakeIdempotencyKey([]string{"myapp", "songbird", "claim", "404"}, "")
resp, err := c.SignTransaction(ctx, fwdclient.SignTransactionRequest{
    Wallet: "claimer-songbird", Chain: 19, To: rewardManager,
    Data: calldata, Gas: 300000, MaxFeePerGas: maxFee, MaxPriorityFeePerGas: tip,
}, idem)
if err != nil {
    if fwdclient.IsRetryable(err) { /* back off + retry */ } else { /* operator action */ }
    return err
}

// 2) Broadcast resp.SignedRawTx yourself (go-ethereum), then report back.
_, _ = c.ReportBroadcastResult(ctx, resp.TxID, fwdclient.BroadcastResultRequest{
    TxHash: resp.Hash, Outcome: fwdclient.OutcomeAccepted,
})
// 3) After it mines:
_, _ = c.ReportReceipt(ctx, resp.TxID, fwdclient.ReceiptRequest{
    TxHash: resp.Hash, Outcome: fwdclient.OutcomeMinedSuccess, BlockNumber: blk,
})
```

FSP signing (Leg-1) — UPTIME omits the reward fields; REWARD_DISTRIBUTION requires
them (validated before any HTTP call):

```go
sig, err := c.SignFspMessage(ctx, fwdclient.SignFspMessageRequest{
    Wallet: "fsp-signing-songbird", MessageType: fwdclient.MessageTypeUptime, RewardEpochID: 404,
}, "")
```

## Errors

Every non-200 response and every transport failure returns an `*Error`
(`Status`, `Code`, `Message`, `Retryable`). Classify with `IsRetryable(err)` /
`IsTerminal(err)`. Terminal: 400/401/403/404/422 and most 409s (auth, policy,
bad request, state conflict). Retryable: 503, network/transport errors, and the
409 `idempotent_replay` code. The client parses fwd's nested `{"detail":{"error",
"message"}}` envelope to surface the real fwd error code.

## Parity

Mirrors the Python `fwd-client` v0.1.0 surface (6 caller methods + the generic
`MakeIdempotencyKey`). `MakeIdempotencyKey` is byte-identical to the Python
helper, so a Go consumer and a Python consumer (clif) dedup the same logical
attempt at fwd. `sign-replacement` is intentionally omitted (not in the Python
lib either); add it when a consumer needs stuck-tx replacement.
