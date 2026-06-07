// Package fwdclient is the keyless Go transport client for the fwd signing daemon.
//
// It is the Go port of the Python fwd-client library and speaks the same
// fwd v1.1.0a9+ zero-egress / sign-only HTTP contract. It is PURE TRANSPORT:
// it does not build calldata, hold keys, sign, or broadcast. The caller signs
// via fwd (POST /v1/sign-transaction or /v1/sign-fsp-message), broadcasts the
// returned signed_raw_tx itself (e.g. via go-ethereum eth_sendRawTransaction),
// then reports the outcome back (broadcast-result + receipt).
//
// Endpoints:
//
//	POST /v1/sign-transaction                     -> SignTransaction
//	POST /v1/sign-fsp-message                     -> SignFspMessage
//	POST /v1/transactions/{id}/broadcast-result   -> ReportBroadcastResult
//	POST /v1/transactions/{id}/receipt            -> ReportReceipt
//	GET  /v1/transactions/{id}                    -> GetTransaction
//	GET  /healthz                                 -> Health
//
// Error taxonomy (mirrors the Python lib): every non-200 response and every
// transport error becomes an *Error. Use IsRetryable / IsTerminal to decide
// whether to back off and retry. Terminal: 400/401/403/404/422 and most 409s.
// Retryable: 503, transport/network errors, and the 409 idempotent_replay code.
//
// Admin operations (wallets/callers/nonce/policy) are NOT part of this client —
// they are operator/admin-keyed (the clifwd CLI), never the keyless caller path.
package fwdclient
