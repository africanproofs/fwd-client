"""Generic idempotency-key helper for fwd callers.

fwd deduplicates signing requests by ``Idempotency-Key`` header (≤128 chars).
The helpers here produce stable, bounded keys from arbitrary string parts —
they carry no clif-specific knowledge (no network table, no claim logic).

Caller-specific composition (e.g. ``f"clif:{network}:{claim_type}:…"``) lives
in the consumer, not here.
"""

from __future__ import annotations

import hashlib

_MAX_KEY_LEN: int = 128


def make_idempotency_key(*parts: str, retry: str | None = None) -> str:
    """Deterministic sha256-based idempotency key from arbitrary string parts.

    The key is stable across processes: the same *parts* (and the same
    optional *retry* discriminator) always produce the same key, so a network
    retry or crash-rerun of the **same logical attempt** dedups at fwd.

    ``retry`` is the operator-controlled discriminator for a **deliberate**
    logical re-attempt after an on-chain failure (fwd replay is status-blind
    by design).  ``retry=None`` ⇒ the key is byte-identical to a key produced
    without the retry argument.  A new ``retry`` value ⇒ a fresh key.  Callers
    should never auto-generate ``retry`` — it is set explicitly by the operator.

    The returned key is ≤128 chars (fwd's limit).
    """
    raw = ":".join(parts)
    if retry is not None:
        raw += f":retry={retry}"
    digest = hashlib.sha256(raw.encode()).hexdigest()
    # Prefix with sanitised parts (first 80 chars) so the key is human-readable
    # in logs; the digest suffix guarantees uniqueness regardless of prefix length.
    prefix = raw[:80].replace(" ", "_")
    key = f"{prefix}-{digest[:16]}"
    # Truncate to the hard limit (prefix may be shorter than 80 chars, so in
    # practice this only fires on very long composite parts).
    return key[:_MAX_KEY_LEN]
