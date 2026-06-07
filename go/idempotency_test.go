package fwdclient

import (
	"fmt"
	"testing"
)

// Golden vectors captured from the live Python fwd-client make_idempotency_key
// (fwd-client v0.1.0). Cross-language parity gate: Go MUST produce byte-identical
// keys so a Go consumer and clif (Python) dedup the same logical attempt at fwd.
// Go retry="" maps to Python retry=None (no discriminator).
func TestMakeIdempotencyKeyGolden(t *testing.T) {
	long := make([]string, 30)
	for i := range long {
		long[i] = fmt.Sprintf("seg%02d", i)
	}
	cases := []struct {
		name  string
		parts []string
		retry string
		want  string
	}{
		{"basic", []string{"clif", "songbird", "2", "404"}, "", "clif:songbird:2:404-c881a5983a4d9488"},
		{"retry", []string{"clif", "songbird", "2", "404"}, "r2", "clif:songbird:2:404:retry=r2-09dc2f33c4c26a1c"},
		{"two", []string{"a", "b"}, "", "a:b-6783a31eabf68ccc"},
		{"space", []string{"with space", "x"}, "", "with_space:x-3dd10fc2bfebffda"},
		{"long_truncates_prefix_to_80", long, "", "seg00:seg01:seg02:seg03:seg04:seg05:seg06:seg07:seg08:seg09:seg10:seg11:seg12:se-193f16a1387c3d78"},
		{"one", []string{"only"}, "", "only-f905b19542ed08c9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MakeIdempotencyKey(tc.parts, tc.retry)
			if got != tc.want {
				t.Fatalf("parity mismatch:\n got  %q\n want %q", got, tc.want)
			}
			if len(got) > maxKeyLen {
				t.Fatalf("key length %d exceeds %d", len(got), maxKeyLen)
			}
		})
	}
}

func TestMakeIdempotencyKeyDeterministic(t *testing.T) {
	a := MakeIdempotencyKey([]string{"x", "y"}, "")
	b := MakeIdempotencyKey([]string{"x", "y"}, "")
	if a != b {
		t.Fatalf("not deterministic: %q != %q", a, b)
	}
	// A retry discriminator yields a different key.
	if MakeIdempotencyKey([]string{"x", "y"}, "r1") == a {
		t.Fatal("retry discriminator did not change the key")
	}
}
