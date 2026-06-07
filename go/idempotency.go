package fwdclient

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const maxKeyLen = 128

// MakeIdempotencyKey is a byte-identical port of the Python fwd-client
// make_idempotency_key: a deterministic, bounded (<=128 char) key derived from
// arbitrary string parts, so a network retry or crash-rerun of the SAME logical
// attempt dedups at fwd. It carries no consumer-specific knowledge — composition
// (e.g. "clif:<network>:<type>:<epoch>") lives in the consumer.
//
// retry is the operator-controlled discriminator for a DELIBERATE re-attempt
// after an on-chain failure. An empty retry ("") means no discriminator and is
// byte-identical to the Python retry=None case; a non-empty retry appends
// ":retry=<retry>" (Python retry=<value>). For identical (parts, retry) inputs,
// Go and Python produce identical keys. (The degenerate Python retry="" case —
// append ":retry=" — is not representable here; use a non-empty tag.)
func MakeIdempotencyKey(parts []string, retry string) string {
	raw := strings.Join(parts, ":")
	if retry != "" {
		raw += ":retry=" + retry
	}
	sum := sha256.Sum256([]byte(raw))
	digest := hex.EncodeToString(sum[:]) // 64 lowercase hex chars
	// Human-readable prefix (first 80 chars of raw, spaces -> underscores) plus
	// the first 16 hex chars of the digest for uniqueness. Slicing is rune-based
	// to match Python str slicing (identical to byte-slicing for ASCII inputs).
	prefix := strings.ReplaceAll(truncRunes(raw, 80), " ", "_")
	key := prefix + "-" + digest[:16]
	return truncRunes(key, maxKeyLen)
}

// truncRunes returns the first n runes of s (Python str[:n] semantics).
func truncRunes(s string, n int) string {
	if len(s) <= n { // fast path: byte len <= n implies rune len <= n
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
